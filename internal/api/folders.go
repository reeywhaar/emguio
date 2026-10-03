package api

import (
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"emguio/internal/store"
)

// folderNameMax bounds a new folder's name, in characters.
const folderNameMax = 200

type folderBody struct {
	Name string `json:"name"`
	// Parent is the folder it goes inside; empty puts it at the top of the user's own.
	Parent string `json:"parent"`
}

// createMailbox makes a folder on the mail server, for something to be moved into: made there,
// then listed and kept here like the rest, and answered with. See docs/reading.md.
func (s *Server) createMailbox(w http.ResponseWriter, r *http.Request) {
	var body folderBody
	if !decode(w, r, &body) {
		return
	}
	u := userOf(r)
	c, err := s.store.EmailConfig(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	boxes, err := s.store.Mailboxes(r.Context(), u.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var parent *store.Mailbox
	if body.Parent != "" {
		if parent, err = s.store.Mailbox(r.Context(), u.ID, c.ID, body.Parent); err != nil {
			s.fail(w, r, err)
			return
		}
		if parent.Delimiter == "" {
			refuse(w, http.StatusBadRequest, CodeInvalid, "This server keeps its folders side by side, not one inside another.")
			return
		}
	}
	name := strings.TrimSpace(body.Name)
	delimiter := ""
	for _, mb := range boxes {
		if mb.Delimiter != "" {
			delimiter = mb.Delimiter
			break
		}
	}
	switch {
	case name == "":
		refuse(w, http.StatusBadRequest, CodeInvalid, "Name the folder.")
		return
	case utf8.RuneCountInString(name) > folderNameMax:
		refuse(w, http.StatusBadRequest, CodeInvalid, "A folder's name is at most 200 characters.")
		return
	case strings.ContainsFunc(name, unicode.IsControl):
		refuse(w, http.StatusBadRequest, CodeInvalid, "A folder's name is one line of text.")
		return
	case delimiter != "" && strings.Contains(name, delimiter):
		refuse(w, http.StatusBadRequest, CodeInvalid, "A folder's name cannot hold “"+delimiter+"”. Choose the folder it goes inside instead.")
		return
	case parent == nil && strings.EqualFold(name, "INBOX"):
		refuse(w, http.StatusConflict, CodeConflict, "There is an INBOX already.")
		return
	}
	for _, mb := range boxes {
		path := mb.Path()
		inside := parent != nil && strings.HasPrefix(mb.Name, parent.Name+parent.Delimiter) && len(path) == len(parent.Path())+1
		top := parent == nil && len(path) == 1
		if (inside || top) && strings.EqualFold(path[len(path)-1], name) {
			refuse(w, http.StatusConflict, CodeConflict, "There is already a folder called "+name+" there.")
			return
		}
	}
	if s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return
	}

	full, listed, err := s.mirror.CreateMailbox(r.Context(), targetOf(u, c), parent, name)
	if !s.serverError(w, r, c.ID, err, "") {
		return
	}
	if _, err := s.store.PutMailboxes(r.Context(), c.ID, listed); err != nil {
		s.fail(w, r, err)
		return
	}
	after, err := s.store.Mailboxes(r.Context(), u.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// By the name it was made with, or else the one folder that was not there before: a server
	// may file it under a name of its own.
	made := slices.IndexFunc(after, func(mb *store.Mailbox) bool { return mb.Name == full })
	if made < 0 {
		made = slices.IndexFunc(after, func(mb *store.Mailbox) bool {
			return !slices.ContainsFunc(boxes, func(was *store.Mailbox) bool { return was.ID == mb.ID })
		})
	}
	if made < 0 {
		refuse(w, http.StatusBadGateway, CodeUnreachable, "The server made the folder and then did not list it.")
		return
	}
	s.store.Notify(u.ID)
	s.mirror.Refresh(c.ID)
	writeJSON(w, http.StatusCreated, mailboxOut(after[made]))
}
