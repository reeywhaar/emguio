package api

import (
	"bytes"
	"io/fs"
	"net/http"
)

// Docs serves the API reference, docs/api.md.
//
// Unauthenticated, and the whole text is in the body: a program does one GET and reads what
// comes back. A page that needed JavaScript would look right in a browser and be empty to a
// reader that never runs any.
type Docs struct {
	html     []byte
	markdown []byte
	// baseURL is substituted for the placeholder, so the curl lines are copy-pasteable as they
	// stand rather than needing a reader to know what to put where.
	baseURL string
}

// docsPlaceholder is what the page's examples say until the server fills in its own address.
const docsPlaceholder = "https://emguio.example.com"

// NewDocs reads the page once, at startup.
//
// The HTML is built from the same markdown by the web build, so the rendered page and the
// source cannot disagree. Either may be missing: a checkout with no bundle still serves the
// markdown.
func NewDocs(built, source fs.FS, baseURL string) *Docs {
	d := &Docs{baseURL: baseURL}
	if built != nil {
		if body, err := fs.ReadFile(built, "docs.html"); err == nil {
			d.html = body
		}
		if body, err := fs.ReadFile(built, "docs.md"); err == nil {
			d.markdown = body
		}
	}
	if d.markdown == nil && source != nil {
		if body, err := fs.ReadFile(source, "api.md"); err == nil {
			d.markdown = body
		}
	}
	return d
}

// ServeHTTP answers with the rendered page, or with the markdown when there is no bundle.
func (d *Docs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	markdown := r.URL.Path == "/docs.md" || len(d.html) == 0
	body := d.html
	if markdown {
		body = d.markdown
	}
	if len(body) == 0 {
		refuse(w, http.StatusNotFound, CodeNotFound, "This build carries no documentation.")
		return
	}
	if markdown {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	// The same for everybody, and it contains nothing about anybody.
	w.Header().Set("Cache-Control", "public, max-age=300")
	if d.baseURL != "" {
		body = bytes.ReplaceAll(body, []byte(docsPlaceholder), []byte(d.baseURL))
	}
	w.Write(body)
}

// llms serves the lines a convention-following client looks for first.
func (s *Server) llms(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	base := s.cfg.PublicURL.String()
	w.Write([]byte("# emguio\n\n" +
		"A self-hosted webmail, reading and sending through the user's own mail accounts.\n\n" +
		"- [API reference](" + base + "/docs.md): signing in, ids, every endpoint, errors\n"))
}
