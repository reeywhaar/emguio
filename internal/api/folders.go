package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"emguio/internal/mirror"
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
	parent, name, ok := s.placeOf(w, r, c, boxes, nil, body)
	if !ok {
		return
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
	s.log.Info("folder made", "user", u.ID, "email_config", c.ID, "mailbox", after[made].ID)
	writeJSON(w, http.StatusCreated, mailboxOut(after[made]))
}

// placeOf reads where a folder goes and what it is called, for one made or, self, renamed: the
// folder it goes inside, nil for the top, and its name, or a refusal already written.
func (s *Server) placeOf(w http.ResponseWriter, r *http.Request, c *store.EmailConfig, boxes []*store.Mailbox, self *store.Mailbox, body folderBody) (*store.Mailbox, string, bool) {
	var parent *store.Mailbox
	if body.Parent != "" {
		var err error
		if parent, err = s.store.Mailbox(r.Context(), userOf(r).ID, c.ID, body.Parent); err != nil {
			s.fail(w, r, err)
			return nil, "", false
		}
		if parent.Delimiter == "" {
			refuse(w, http.StatusBadRequest, CodeInvalid, "This server keeps its folders side by side, not one inside another.")
			return nil, "", false
		}
		if self != nil && (parent.ID == self.ID || inside(parent, self)) {
			refuse(w, http.StatusBadRequest, CodeInvalid, "A folder cannot go inside itself.")
			return nil, "", false
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
		return nil, "", false
	case utf8.RuneCountInString(name) > folderNameMax:
		refuse(w, http.StatusBadRequest, CodeInvalid, "A folder's name is at most 200 characters.")
		return nil, "", false
	case strings.ContainsFunc(name, unicode.IsControl):
		refuse(w, http.StatusBadRequest, CodeInvalid, "A folder's name is one line of text.")
		return nil, "", false
	case delimiter != "" && strings.Contains(name, delimiter):
		refuse(w, http.StatusBadRequest, CodeInvalid, "A folder's name cannot hold “"+delimiter+"”. Choose the folder it goes inside instead.")
		return nil, "", false
	case parent == nil && strings.EqualFold(name, "INBOX"):
		refuse(w, http.StatusConflict, CodeConflict, "There is an INBOX already.")
		return nil, "", false
	}
	for _, mb := range boxes {
		if self != nil && mb.ID == self.ID {
			continue
		}
		path := mb.Path()
		in := parent != nil && inside(mb, parent) && len(path) == len(parent.Path())+1
		top := parent == nil && len(path) == 1
		if (in || top) && strings.EqualFold(path[len(path)-1], name) {
			refuse(w, http.StatusConflict, CodeConflict, "There is already a folder called "+name+" there.")
			return nil, "", false
		}
	}
	return parent, name, true
}

// inside is whether mb is somewhere inside of.
func inside(mb, of *store.Mailbox) bool {
	return of.Delimiter != "" && strings.HasPrefix(mb.Name, of.Name+of.Delimiter)
}

// owned is a folder that is the server's own, for a purpose, which keeps its name and place.
func owned(w http.ResponseWriter, mb *store.Mailbox) bool {
	if mb.SpecialUse == "" {
		return false
	}
	refuse(w, http.StatusConflict, CodeConflict, "Inbox and the server's own folders keep their names and places.")
	return true
}

// renameMailbox renames a folder on the mail server, or moves it inside another or to the top, or
// both; what is inside it goes with it, and here every one keeps its id. See docs/reading.md.
func (s *Server) renameMailbox(w http.ResponseWriter, r *http.Request) {
	var body folderBody
	if !decode(w, r, &body) {
		return
	}
	c, mb, ok := s.mailboxOf(w, r)
	if !ok || owned(w, mb) {
		return
	}
	u := userOf(r)
	boxes, err := s.store.Mailboxes(r.Context(), u.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	parent, name, ok := s.placeOf(w, r, c, boxes, mb, body)
	if !ok {
		return
	}
	if s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return
	}
	full, listed, err := s.mirror.RenameMailbox(r.Context(), targetOf(u, c), mb.Name, parent, name)
	if !s.serverError(w, r, c.ID, err, "") {
		return
	}
	if err := s.store.RenameMailboxes(r.Context(), c.ID, mb.Name, full, mb.Delimiter); err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.PutMailboxes(r.Context(), c.ID, listed); err != nil {
		s.fail(w, r, err)
		return
	}
	now, err := s.store.Mailbox(r.Context(), u.ID, c.ID, mb.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.store.Notify(u.ID)
	s.mirror.Refresh(c.ID)
	s.log.Info("folder renamed or moved", "user", u.ID, "email_config", c.ID, "mailbox", now.ID)
	writeJSON(w, http.StatusOK, mailboxOut(now))
}

// deleteMailbox deletes an empty folder from the mail server, and from here. One that holds mail,
// or other folders, is refused: deleting it would delete them.
func (s *Server) deleteMailbox(w http.ResponseWriter, r *http.Request) {
	c, mb, ok := s.mailboxOf(w, r)
	if !ok || owned(w, mb) {
		return
	}
	u := userOf(r)
	boxes, err := s.store.Mailboxes(r.Context(), u.ID, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if slices.ContainsFunc(boxes, func(other *store.Mailbox) bool { return inside(other, mb) }) {
		refuse(w, http.StatusConflict, CodeConflict, "Move or delete the folders inside it first.")
		return
	}
	if s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return
	}
	listed, err := s.mirror.DeleteMailbox(r.Context(), targetOf(u, c), mb.Name)
	if errors.Is(err, mirror.ErrNotEmpty) {
		refuse(w, http.StatusConflict, CodeConflict, "Move or delete the mail in it first.")
		return
	}
	if !s.serverError(w, r, c.ID, err, "") {
		return
	}
	if _, err := s.store.PutMailboxes(r.Context(), c.ID, listed); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("folder deleted", "user", u.ID, "email_config", c.ID, "mailbox", mb.ID)
	s.store.Notify(u.ID)
	s.mirror.Refresh(c.ID)
	w.WriteHeader(http.StatusNoContent)
}

type orderBody struct {
	IDs []string `json:"ids"`
}

// orderMailboxes puts folders side by side in the order given, kept here: the server keeps no
// order. Answered with every folder, in the new order.
func (s *Server) orderMailboxes(w http.ResponseWriter, r *http.Request) {
	var body orderBody
	if !decode(w, r, &body) {
		return
	}
	u := userOf(r)
	if err := s.store.SetMailboxOrder(r.Context(), u.ID, r.PathValue("id"), body.IDs); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("folders put in order", "user", u.ID, "email_config", r.PathValue("id"), "folders", len(body.IDs))
	s.store.Notify(u.ID)
	s.listMailboxes(w, r)
}
