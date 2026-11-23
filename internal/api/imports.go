package api

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/connect"
	"emguio/internal/ids"
	"emguio/internal/mbox"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

// importMax bounds an upload. It is written to a file as it comes, never held in memory.
const importMax = 10 << 30

// importLife is how long a finished import is listed when nobody dismisses it.
const importLife = 24 * time.Hour

// What an import has come to.
const (
	importImporting = "importing"
	importDone      = "done"
	importFailed    = "failed"
	importCancelled = "cancelled"
)

// The kinds of file an import reads, by content type.
var importKinds = map[string]string{
	"application/mbox": "mbox",
	"application/zip":  "zip",
	"message/rfc822":   "eml",
}

// importing is mail put into a folder from a file, in the background: the file uploaded first,
// to disk, then each message read from it and appended to the server. See docs/reading.md.
type importing struct {
	id, user, config, mailbox string
	// file is the name it was uploaded under.
	file string
	made time.Time

	mu       sync.Mutex
	state    string
	total    int
	imported int
	skipped  int
	folders  int
	reason   string
	cancel   context.CancelFunc
	stopping bool
}

type imports struct {
	mu sync.Mutex
	m  map[string]*importing
}

func (x *imports) put(i *importing) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.m == nil {
		x.m = map[string]*importing{}
	}
	x.m[i.id] = i
}

func (x *imports) get(user, id string) *importing {
	x.mu.Lock()
	defer x.mu.Unlock()
	i := x.m[id]
	if i == nil || i.user != user {
		return nil
	}
	return i
}

// of is a user's imports, oldest first; finished ones past their day are let go.
func (x *imports) of(user string) []*importing {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []*importing
	for id, i := range x.m {
		i.mu.Lock()
		stale := i.state != importImporting && time.Since(i.made) > importLife
		i.mu.Unlock()
		if stale {
			delete(x.m, id)
			continue
		}
		if i.user == user {
			out = append(out, i)
		}
	}
	slices.SortFunc(out, func(a, b *importing) int { return a.made.Compare(b.made) })
	return out
}

func (x *imports) drop(id string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	delete(x.m, id)
}

type importJSON struct {
	ID          string `json:"id"`
	EmailConfig string `json:"email_config"`
	Mailbox     string `json:"mailbox"`
	File        string `json:"file"`
	// State is importing, done, failed or cancelled.
	State string `json:"state"`
	// Total is how many messages the file holds; Imported how many went in, Skipped how many were
	// there already, Folders how many were made.
	Total     int    `json:"total"`
	Imported  int    `json:"imported"`
	Skipped   int    `json:"skipped"`
	Folders   int    `json:"folders"`
	Error     string `json:"error"`
	CreatedAt int64  `json:"created_at"`
}

func (i *importing) out() importJSON {
	i.mu.Lock()
	defer i.mu.Unlock()
	return importJSON{
		ID: i.id, EmailConfig: i.config, Mailbox: i.mailbox, File: i.file, State: i.state,
		Total: i.total, Imported: i.imported, Skipped: i.skipped, Folders: i.folders,
		Error: i.reason, CreatedAt: i.made.Unix(),
	}
}

func (i *importing) end(state, why string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.state, i.reason = state, why
}

// importMail takes a file and imports it into a folder in the background: an mbox or an eml into
// the folder, a zip as a tree inside it. The body is the file itself, written to disk as it
// comes.
func (s *Server) importMail(w http.ResponseWriter, r *http.Request) {
	c, mb, ok := s.mailboxOf(w, r)
	if !ok {
		return
	}
	name := path.Base(strings.ReplaceAll(r.URL.Query().Get("name"), "\\", "/"))
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	kind := importKinds[ct]
	if kind == "" {
		switch strings.ToLower(path.Ext(name)) {
		case ".mbox":
			kind = "mbox"
		case ".zip":
			kind = "zip"
		case ".eml":
			kind = "eml"
		}
	}
	switch {
	case kind == "":
		refuse(w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Import an .mbox, an .eml, or a .zip of them.")
		return
	case !mb.Selectable:
		refuse(w, http.StatusBadRequest, CodeInvalid, "This folder only holds other folders. Import into one that holds mail.")
		return
	case s.mirror == nil:
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return
	}

	f, err := os.CreateTemp("", "emguio-import-*")
	if err != nil {
		s.fail(w, r, fmt.Errorf("import: %w", err))
		return
	}
	_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, importMax))
	if err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		os.Remove(f.Name())
		var over *http.MaxBytesError
		if errors.As(err, &over) {
			refuse(w, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, fmt.Sprintf("A file to import is at most %d GB.", importMax>>30))
			return
		}
		refuse(w, http.StatusBadRequest, CodeInvalid, "The file did not arrive whole.")
		return
	}

	u := userOf(r)
	if name == "." || name == "/" {
		name = "mail." + kind
	}
	i := &importing{
		id: ids.New(ids.Import, s.store.Now().UnixMilli()), user: u.ID, config: c.ID, mailbox: mb.ID,
		file: name, made: time.Now(), state: importImporting,
	}
	s.imports.put(i)
	t := targetOf(u, c)
	file := f.Name()
	s.log.Info("import started", "user", u.ID, "email_config", c.ID, "mailbox", mb.ID, "import", i.id, "kind", kind)
	s.mirror.Go(func(life context.Context) {
		ctx, cancel := context.WithCancel(life)
		defer cancel()
		i.mu.Lock()
		i.cancel = cancel
		if i.stopping {
			cancel()
		}
		i.mu.Unlock()
		defer os.Remove(file)
		s.runImport(ctx, t, i, mb, file, kind)
	})
	writeJSON(w, http.StatusAccepted, i.out())
}

// runImport reads the file and appends what it holds, and says how it ended.
func (s *Server) runImport(ctx context.Context, t store.SyncTarget, i *importing, into *store.Mailbox, file, kind string) {
	began := time.Now()
	err := s.importFile(ctx, t, i, into, file, kind)
	out := i.out()
	switch {
	case err == nil:
		i.end(importDone, "")
	case ctx.Err() != nil:
		i.end(importCancelled, "")
	default:
		i.end(importFailed, importProblem(err))
	}
	s.store.Notify(t.UserID)
	s.mirror.Refresh(t.ID)
	attrs := []any{"user", t.UserID, "email_config", t.ID, "import", i.id, "imported", out.Imported,
		"skipped", out.Skipped, "folders", out.Folders, "took", time.Since(began).Round(time.Millisecond)}
	if err != nil && ctx.Err() == nil {
		s.log.Warn("import stopped", append(attrs, "why", importProblem(err), "error", err.Error())...)
		return
	}
	s.log.Info("import ended", append(attrs, "state", i.out().State)...)
}

// importProblem is why an import stopped, in a sentence.
func importProblem(err error) string {
	var f *connect.Failure
	switch {
	case errors.As(err, &f):
		return f.Sentence
	case errors.Is(err, mirror.ErrGone):
		return "The folder is no longer on the server."
	case errors.Is(err, store.ErrInvalid), errors.Is(err, store.ErrNotFound):
		return err.Error()
	case errors.Is(err, zip.ErrFormat), errors.Is(err, zip.ErrAlgorithm):
		return "The zip could not be read."
	}
	return "The mail server stopped answering. Import again: what went in already is passed over."
}

// importFile reads an mbox, an eml or a zip of them, and appends each message.
func (s *Server) importFile(ctx context.Context, t store.SyncTarget, i *importing, into *store.Mailbox, file, kind string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	switch kind {
	case "eml":
		i.mu.Lock()
		i.total = 1
		i.mu.Unlock()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		return s.importOne(ctx, t, i, into.Name, section(f, 0, info.Size()), false, nameDate(i.file))
	case "mbox":
		spans, err := mbox.Scan(f)
		if err != nil {
			return err
		}
		i.mu.Lock()
		i.total = len(spans)
		i.mu.Unlock()
		for _, sp := range spans {
			if err := s.importOne(ctx, t, i, into.Name, section(f, sp.Start, sp.End-sp.Start), true, sp.Date); err != nil {
				return err
			}
		}
		return nil
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}
	var entries []entry
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		entries = append(entries, entry{name: zf.Name, open: zf.Open})
	}
	return s.importItems(ctx, t, i, into, plan(entries))
}

// importItems counts what the files hold, then imports each into the folder its place names.
func (s *Server) importItems(ctx context.Context, t store.SyncTarget, i *importing, into *store.Mailbox, items []zipItem) error {
	total := 0
	for _, it := range items {
		if it.eml {
			total++
			continue
		}
		rc, err := it.open()
		if err != nil {
			return err
		}
		spans, err := mbox.Scan(rc)
		rc.Close()
		if err != nil {
			return err
		}
		total += len(spans)
	}
	i.mu.Lock()
	i.total = total
	i.mu.Unlock()
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		folder, err := s.folderAt(ctx, t, i, into, it.path)
		if err != nil {
			return err
		}
		if it.eml {
			opened := func() (io.Reader, func(), error) {
				rc, err := it.open()
				if err != nil {
					return nil, nil, err
				}
				return rc, func() { rc.Close() }, nil
			}
			if err := s.importOne(ctx, t, i, folder.Name, opened, false, it.date); err != nil {
				return err
			}
			continue
		}
		if err := s.importMboxEntry(ctx, t, i, folder.Name, it.entry); err != nil {
			return err
		}
	}
	return nil
}

// importMboxEntry imports an mbox from a zip: written out to a file of its own first, since each
// message is read twice and a compressed one cannot be read from the middle.
func (s *Server) importMboxEntry(ctx context.Context, t store.SyncTarget, i *importing, mailbox string, e entry) error {
	out, err := os.CreateTemp("", "emguio-import-mbox-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	rc, err := e.open()
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	rc.Close()
	if err != nil {
		return err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return err
	}
	spans, err := mbox.Scan(out)
	if err != nil {
		return err
	}
	for _, sp := range spans {
		if err := s.importOne(ctx, t, i, mailbox, section(out, sp.Start, sp.End-sp.Start), true, sp.Date); err != nil {
			return err
		}
	}
	return nil
}

// opener opens a message's text afresh each time: once to measure it, once to send it.
type opener func() (io.Reader, func(), error)

func section(f *os.File, start, n int64) opener {
	return func() (io.Reader, func(), error) {
		return io.NewSectionReader(f, start, n), func() {}, nil
	}
}

// importOne appends one message, unless the folder has it already. Read and the rest come from
// its Status and X-Status; one without them is read, as an archive is. When it arrived is the
// mbox's From_ line's, or an eml's name's, or else its Date.
func (s *Server) importOne(ctx context.Context, t store.SyncTarget, i *importing, mailbox string, open opener, isMbox bool, arrived time.Time) error {
	r, done, err := open()
	if err != nil {
		return err
	}
	h, err := mbox.Convert(r, io.Discard, isMbox)
	done()
	if err != nil {
		return err
	}
	flags := []imap.Flag{}
	if h.Seen || !h.Status {
		flags = append(flags, imap.FlagSeen)
	}
	if h.Answered {
		flags = append(flags, imap.FlagAnswered)
	}
	if h.Flagged {
		flags = append(flags, imap.FlagFlagged)
	}
	if h.Draft {
		flags = append(flags, imap.FlagDraft)
	}
	if arrived.IsZero() {
		arrived = h.Date
	}
	put, err := s.mirror.Append(ctx, t, mailbox, mirror.Appending{
		Size: h.Size, MessageID: h.MessageID, Flags: flags, Date: arrived,
		Write: func(w io.Writer) error {
			r, done, err := open()
			if err != nil {
				return err
			}
			defer done()
			_, err = mbox.Convert(r, w, isMbox)
			return err
		},
	})
	if err != nil {
		return err
	}
	i.mu.Lock()
	if put {
		i.imported++
	} else {
		i.skipped++
	}
	i.mu.Unlock()
	return nil
}

// entry is a file of a zip to import from: its place there, slashes between, and how to read it.
type entry struct {
	name string
	open func() (io.ReadCloser, error)
}

// zipItem is a file to import: an mbox, or one eml, and the folder it goes in.
type zipItem struct {
	entry
	// path is the folder under the one imported into, nil for that one.
	path []string
	eml  bool
	date time.Time
}

// plan reads what a zip holds: .mbox files, Mail's Name.mbox/mbox, and .eml
// files, each in the folder its place names. Directories named Name.sbd or Name.mbox are the
// folder Name. When everything is under one name, that name is the folder imported into.
func plan(entries []entry) []zipItem {
	var items []zipItem
	folder := func(dir string) string {
		return strings.TrimSuffix(strings.TrimSuffix(dir, ".sbd"), ".mbox")
	}
	for _, f := range entries {
		parts := strings.Split(strings.Trim(strings.ReplaceAll(f.name, "\\", "/"), "/"), "/")
		last := parts[len(parts)-1]
		if slices.Contains(parts, "__MACOSX") || strings.HasPrefix(last, "._") || slices.Contains(parts, "..") {
			continue
		}
		var dirs []string
		for _, d := range parts[:len(parts)-1] {
			dirs = append(dirs, folder(d))
		}
		lower := strings.ToLower(last)
		switch {
		case strings.HasSuffix(lower, ".eml"):
			items = append(items, zipItem{entry: f, path: dirs, eml: true, date: nameDate(last)})
		case strings.HasSuffix(lower, ".mbox"):
			items = append(items, zipItem{entry: f, path: append(dirs, last[:len(last)-len(".mbox")])})
		case last == "mbox" && len(parts) > 1 && strings.HasSuffix(parts[len(parts)-2], ".mbox"):
			items = append(items, zipItem{entry: f, path: dirs})
		}
	}
	roots := map[string]bool{}
	for _, it := range items {
		if len(it.path) == 0 {
			roots[""] = true
		} else {
			roots[it.path[0]] = true
		}
	}
	if len(roots) == 1 && !roots[""] {
		for i := range items {
			items[i].path = items[i].path[1:]
		}
	}
	return items
}

// emlName is how an export names an eml: when it arrived, then its UID.
var emlName = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}-\d{6})-`)

// nameDate is when an eml arrived as its name says, zero when it does not.
func nameDate(name string) time.Time {
	m := emlName.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}
	}
	at, err := time.Parse("2006-01-02-150405", m[1])
	if err != nil {
		return time.Time{}
	}
	return at
}

// folderAt is the folder at path under into, each step of it made on the server where it is not
// there yet.
func (s *Server) folderAt(ctx context.Context, t store.SyncTarget, i *importing, into *store.Mailbox, path []string) (*store.Mailbox, error) {
	at := into
	for _, name := range path {
		if at.Delimiter == "" {
			return nil, store.Invalid("This mail server keeps no folders inside folders, so the zip's folders cannot be made. Import its files one at a time.")
		}
		name = strings.ReplaceAll(name, at.Delimiter, "_")
		boxes, err := s.store.MirrorMailboxes(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		child := func(boxes []*store.Mailbox) *store.Mailbox {
			for _, b := range boxes {
				if inside(b, at) && len(b.Path()) == len(at.Path())+1 && b.Path()[len(b.Path())-1] == name {
					return b
				}
			}
			return nil
		}
		next := child(boxes)
		if next == nil {
			full, listed, err := s.mirror.CreateMailbox(ctx, t, at, name)
			if err != nil {
				return nil, err
			}
			if _, err := s.store.PutMailboxes(ctx, t.ID, listed); err != nil {
				return nil, err
			}
			after, err := s.store.MirrorMailboxes(ctx, t.ID)
			if err != nil {
				return nil, err
			}
			if k := slices.IndexFunc(after, func(b *store.Mailbox) bool { return b.Name == full }); k >= 0 {
				next = after[k]
			} else if next = child(after); next == nil {
				return nil, store.NotFound("The server made a folder and then did not list it.")
			}
			i.mu.Lock()
			i.folders++
			i.mu.Unlock()
		}
		at = next
	}
	return at, nil
}

// listImports is the user's imports, running and finished, oldest first.
func (s *Server) listImports(w http.ResponseWriter, r *http.Request) {
	list := s.imports.of(userOf(r).ID)
	out := make([]importJSON, len(list))
	for k, i := range list {
		out[k] = i.out()
	}
	writeJSON(w, http.StatusOK, map[string]any{"imports": out})
}

// cancelImport stops an import: what went in stays, the message under way finishes or not.
func (s *Server) cancelImport(w http.ResponseWriter, r *http.Request) {
	i := s.imports.get(userOf(r).ID, r.PathValue("import"))
	if i == nil {
		refuse(w, http.StatusNotFound, CodeNotFound, "There is no such import.")
		return
	}
	i.mu.Lock()
	i.stopping = true
	if i.cancel != nil {
		i.cancel()
	}
	i.mu.Unlock()
	writeJSON(w, http.StatusOK, i.out())
}

// dismissImport lets a finished import go, once it has been shown.
func (s *Server) dismissImport(w http.ResponseWriter, r *http.Request) {
	i := s.imports.get(userOf(r).ID, r.PathValue("import"))
	if i == nil {
		refuse(w, http.StatusNotFound, CodeNotFound, "There is no such import.")
		return
	}
	if i.out().State == importImporting {
		refuse(w, http.StatusBadRequest, CodeInvalid, "It is still importing: cancel it first.")
		return
	}
	s.imports.drop(i.id)
	w.WriteHeader(http.StatusNoContent)
}
