package api

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"emguio/internal/connect"
	"emguio/internal/ids"
	"emguio/internal/mbox"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

// exportLife is how long an export is held: asked for, downloaded, and what went into it deleted
// if the user chooses. In memory, so a restart forgets it and the folder is exported again.
const exportLife = time.Hour

// exportMax bounds a selection exported at once.
const exportMax = 10000

// What an export has come to.
const (
	exportReady     = "ready"
	exportExporting = "exporting"
	exportDone      = "done"
	exportFailed    = "failed"
	exportDeleted   = "deleted"
)

// export is a download of mail asked for and then fetched at an address of its own, so the
// browser saves it as it streams. What went into it is kept, for deleting exactly that after.
// See docs/reading.md.
type export struct {
	id, user, config string
	// format is mbox or eml.
	format string
	// boxes is the folder exported, then those inside it when they go too.
	boxes []*store.Mailbox
	// uids and uidValidity are a selection of the first folder's messages; nil for all of them.
	uids        []uint32
	uidValidity uint32
	made        time.Time

	mu     sync.Mutex
	state  string
	total  int
	done   int
	reason string
	// took is what went into the file, by folder id.
	took map[string]taken
}

type taken struct {
	uidValidity uint32
	uids        []uint32
}

type exports struct {
	mu sync.Mutex
	m  map[string]*export
}

func (x *exports) put(e *export) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.m == nil {
		x.m = map[string]*export{}
	}
	for id, old := range x.m {
		if time.Since(old.made) > exportLife {
			delete(x.m, id)
		}
	}
	x.m[e.id] = e
}

func (x *exports) get(user, id string) *export {
	x.mu.Lock()
	defer x.mu.Unlock()
	e := x.m[id]
	if e == nil || e.user != user || time.Since(e.made) > exportLife {
		return nil
	}
	return e
}

type exportJSON struct {
	ID string `json:"id"`
	// State is ready, exporting, done, failed or deleted.
	State string `json:"state"`
	// Total is how many messages are to go, as last counted; Exported how many have.
	Total    int    `json:"total"`
	Exported int    `json:"exported"`
	Error    string `json:"error"`
	// File is the name it is saved under, and URL where it is downloaded from, once.
	File string `json:"file"`
	URL  string `json:"url"`
}

func (e *export) out() exportJSON {
	e.mu.Lock()
	defer e.mu.Unlock()
	return exportJSON{
		ID: e.id, State: e.state, Total: e.total, Exported: e.done, Error: e.reason,
		File: e.file(), URL: "/api/exports/" + e.id + "/file",
	}
}

// zipped is whether the file is a zip: eml always but for one message, mbox with subfolders.
func (e *export) zipped() bool {
	if e.format == "eml" {
		return len(e.uids) != 1
	}
	return len(e.boxes) > 1
}

func (e *export) file() string {
	name := fileName(e.boxes[0])
	switch {
	case e.zipped():
		return name + ".zip"
	case e.format == "eml":
		return name + ".eml"
	}
	return name + ".mbox"
}

type exportBody struct {
	// Format is mbox or eml.
	Format string `json:"format"`
	// Messages is a selection of the folder's; none exports all of it.
	Messages []string `json:"messages"`
	// Subfolders exports the folders inside it too.
	Subfolders bool `json:"subfolders"`
}

// createExport asks for a folder's mail, or a selection of it, as a file to download.
func (s *Server) createExport(w http.ResponseWriter, r *http.Request) {
	c, mb, ok := s.mailboxOf(w, r)
	if !ok {
		return
	}
	var body exportBody
	if !decode(w, r, &body) {
		return
	}
	switch {
	case body.Format != "mbox" && body.Format != "eml":
		refuse(w, http.StatusBadRequest, CodeInvalid, "Export as mbox or eml.")
		return
	case len(body.Messages) > 0 && body.Subfolders:
		refuse(w, http.StatusBadRequest, CodeInvalid, "A selection is exported without subfolders.")
		return
	case len(body.Messages) > exportMax:
		refuse(w, http.StatusBadRequest, CodeInvalid, fmt.Sprintf("Export at most %d messages at once.", exportMax))
		return
	}
	u := userOf(r)
	e := &export{
		id: ids.New(ids.Export, s.store.Now().UnixMilli()), user: u.ID, config: c.ID,
		format: body.Format, boxes: []*store.Mailbox{mb}, made: time.Now(), state: exportReady,
		took: map[string]taken{},
	}
	for _, id := range body.Messages {
		v, uid, ok := ids.ParseMessage(id)
		if !ok || (e.uidValidity != 0 && v != e.uidValidity) {
			refuse(w, http.StatusBadRequest, CodeInvalid, fmt.Sprintf("%q is not a message of this folder.", id))
			return
		}
		e.uidValidity = v
		e.uids = append(e.uids, uid)
	}
	if body.Subfolders {
		boxes, err := s.store.MirrorMailboxes(r.Context(), c.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		for _, other := range boxes {
			if inside(other, mb) {
				e.boxes = append(e.boxes, other)
			}
		}
		slices.SortFunc(e.boxes[1:], func(a, b *store.Mailbox) int { return strings.Compare(a.Name, b.Name) })
	}
	if !slices.ContainsFunc(e.boxes, func(b *store.Mailbox) bool { return b.Selectable }) {
		refuse(w, http.StatusBadRequest, CodeInvalid, "This folder only holds other folders.")
		return
	}
	if e.uids != nil {
		e.total = len(e.uids)
	} else {
		for _, b := range e.boxes {
			e.total += int(b.Messages)
		}
	}
	s.exports.put(e)
	writeJSON(w, http.StatusCreated, e.out())
}

func (s *Server) getExport(w http.ResponseWriter, r *http.Request) {
	e := s.exports.get(userOf(r).ID, r.PathValue("export"))
	if e == nil {
		refuse(w, http.StatusNotFound, CodeNotFound, "There is no such export: it is downloaded within an hour of being asked for.")
		return
	}
	writeJSON(w, http.StatusOK, e.out())
}

// counting is a response that knows whether anything of it has been written: a failure before
// is a refusal, after it a download cut short.
type counting struct {
	http.ResponseWriter
	wrote bool
}

func (c *counting) Write(b []byte) (int, error) {
	c.wrote = true
	return c.ResponseWriter.Write(b)
}

// exportFile streams the file, once: each message read from the server and written on as it
// comes, none held whole. A failure partway cuts the download short, so the browser does not
// keep half a file as if it were whole.
func (s *Server) exportFile(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	e := s.exports.get(u.ID, r.PathValue("export"))
	if e == nil {
		refuse(w, http.StatusNotFound, CodeNotFound, "There is no such export: it is downloaded within an hour of being asked for.")
		return
	}
	e.mu.Lock()
	if e.state != exportReady {
		e.mu.Unlock()
		refuse(w, http.StatusConflict, CodeConflict, "This export has been downloaded already. Export again.")
		return
	}
	e.state = exportExporting
	e.mu.Unlock()
	c, err := s.store.EmailConfig(r.Context(), u.ID, e.config)
	if err != nil {
		e.fail(err.Error())
		s.fail(w, r, err)
		return
	}
	if s.mirror == nil {
		e.fail("The server cannot be reached right now.")
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return
	}

	kind := "application/mbox"
	switch {
	case e.zipped():
		kind = "application/zip"
	case e.format == "eml":
		kind = "message/rfc822"
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": e.file()}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	out := &counting{ResponseWriter: w}
	began := time.Now()
	err = s.writeExport(r, targetOf(u, c), e, out)
	if err != nil {
		why := s.exportProblem(r, err)
		e.fail(why)
		s.log.Warn("export stopped", "user", u.ID, "email_config", c.ID, "export", e.id,
			"messages", e.out().Exported, "why", why, "error", err.Error())
		if !out.wrote {
			s.serverError(w, r, c.ID, err, "The folder is no longer on the server.")
			return
		}
		// The download is cut short, so the browser marks it failed rather than whole.
		panic(http.ErrAbortHandler)
	}
	e.mu.Lock()
	e.state = exportDone
	e.total = e.done
	e.mu.Unlock()
	s.log.Info("mail exported", "user", u.ID, "email_config", c.ID, "export", e.id,
		"format", e.format, "folders", len(e.boxes), "messages", e.out().Exported,
		"took", time.Since(began).Round(time.Millisecond))
}

func (e *export) fail(why string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state, e.reason = exportFailed, why
}

// writeExport writes each folder's messages to out in the export's format.
func (s *Server) writeExport(r *http.Request, t store.SyncTarget, e *export, out io.Writer) error {
	var zw *zip.Writer
	if e.zipped() {
		zw = zip.NewWriter(out)
	}
	root := e.boxes[0].Path()
	for _, b := range e.boxes {
		if !b.Selectable {
			continue
		}
		// Where it sits under the folder exported, which is named alone.
		path := []string{fileName(e.boxes[0])}
		for _, part := range b.Path()[len(root):] {
			path = append(path, safeName(part))
		}
		dir := strings.Join(path, "/")
		var mw *mbox.Writer
		if e.format == "mbox" {
			to := out
			if zw != nil {
				entry, err := zw.CreateHeader(&zip.FileHeader{Name: dir + ".mbox", Method: zip.Deflate, Modified: time.Now()})
				if err != nil {
					return err
				}
				to = entry
			}
			mw = mbox.NewWriter(to)
		}
		var uids []uint32
		each := func(m mirror.Exporting, body io.Reader) error {
			var err error
			switch {
			case mw != nil:
				err = mw.Write(mbox.Message{
					From: m.From, Date: m.Date,
					Seen: m.Seen, Answered: m.Answered, Flagged: m.Flagged, Draft: m.Draft,
				}, body)
			case zw != nil:
				var entry io.Writer
				entry, err = zw.CreateHeader(&zip.FileHeader{
					Name:   fmt.Sprintf("%s/%s-%d.eml", dir, m.Date.UTC().Format("2006-01-02-150405"), m.UID),
					Method: zip.Deflate, Modified: m.Date,
				})
				if err == nil {
					_, err = io.Copy(entry, body)
				}
			default:
				_, err = io.Copy(out, body)
			}
			if err != nil {
				return err
			}
			uids = append(uids, m.UID)
			e.mu.Lock()
			e.done++
			e.mu.Unlock()
			return nil
		}
		// A selection is of the folder exported, which is first.
		var only []uint32
		var under uint32
		if b == e.boxes[0] {
			only, under = e.uids, e.uidValidity
		}
		uidValidity, err := s.mirror.Export(r.Context(), t, b.Name, under, only, each)
		if mw != nil {
			if ferr := mw.Flush(); err == nil {
				err = ferr
			}
		}
		e.mu.Lock()
		e.took[b.ID] = taken{uidValidity: uidValidity, uids: uids}
		e.mu.Unlock()
		if err != nil {
			return err
		}
	}
	if zw != nil {
		return zw.Close()
	}
	return nil
}

// exportProblem is why an export stopped, in a sentence.
func (s *Server) exportProblem(r *http.Request, err error) string {
	var f *connect.Failure
	switch {
	case r.Context().Err() != nil:
		return "The download was stopped."
	case errors.Is(err, mirror.ErrGone):
		return "A folder is no longer on the server as it was. Export again."
	case errors.As(err, &f):
		return f.Sentence
	case errors.Is(err, store.ErrConflict):
		return err.Error()
	}
	return "The mail server stopped answering. Export again."
}

type deleteExportedBody struct {
	// Read marks them read first, where they go to Trash.
	Read bool `json:"read"`
}

// deleteExported deletes what went into a finished export and nothing else: to Trash, as
// deleting does, or for good from Trash or where there is none.
func (s *Server) deleteExported(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	e := s.exports.get(u.ID, r.PathValue("export"))
	if e == nil {
		refuse(w, http.StatusNotFound, CodeNotFound, "There is no such export: it is downloaded within an hour of being asked for.")
		return
	}
	var body deleteExportedBody
	if !decode(w, r, &body) {
		return
	}
	e.mu.Lock()
	if e.state != exportDone {
		e.mu.Unlock()
		refuse(w, http.StatusConflict, CodeConflict, "Only what an export finished with is deleted.")
		return
	}
	e.state = exportDeleted
	took := e.took
	e.mu.Unlock()
	c, err := s.store.EmailConfig(r.Context(), u.ID, e.config)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return
	}
	boxes, err := s.store.MirrorMailboxes(r.Context(), c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	trash := ""
	for _, b := range boxes {
		if b.SpecialUse == store.UseTrash && b.Selectable {
			trash = b.Name
		}
	}
	t := targetOf(u, c)
	var toTrash, forGood int
	for _, b := range e.boxes {
		k, ok := took[b.ID]
		if !ok || len(k.uids) == 0 {
			continue
		}
		to := trash
		if b.SpecialUse == store.UseTrash {
			to = ""
		}
		gone, err := s.mirror.Discard(r.Context(), t, b.Name, k.uidValidity, k.uids, to, body.Read && to != "")
		if !s.serverError(w, r, c.ID, err, "A folder is no longer on the server as it was exported.") {
			// What went before stays gone; the rest can be tried again.
			e.mu.Lock()
			e.state = exportDone
			e.mu.Unlock()
			return
		}
		if err := s.store.DeleteMessages(r.Context(), b.ID, gone); err != nil {
			s.fail(w, r, err)
			return
		}
		if to != "" {
			toTrash += len(gone)
		} else {
			forGood += len(gone)
		}
	}
	s.store.Notify(u.ID)
	s.mirror.Refresh(c.ID)
	s.log.Info("exported mail deleted", "user", u.ID, "email_config", c.ID, "export", e.id,
		"to_trash", toTrash, "for_good", forGood)
	writeJSON(w, http.StatusOK, map[string]any{"trash": toTrash, "deleted": forGood})
}

// fileName is a folder's name as a file is named after it: Inbox for INBOX.
func fileName(mb *store.Mailbox) string {
	path := mb.Path()
	if mb.SpecialUse == store.UseInbox && len(path) == 1 {
		return "Inbox"
	}
	return safeName(path[len(path)-1])
}

// safeName is a folder's name made fit for a file's: no separators or control characters, and
// not empty or only dots.
func safeName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f, strings.ContainsRune(`/\:*?"<>|`, r):
			return '_'
		}
		return r
	}, name)
	if strings.Trim(name, ". ") == "" {
		return "Folder"
	}
	return name
}
