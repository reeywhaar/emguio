package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPA serves the built frontend from memory: every file read once at construction, ETag and
// content type precomputed. Read from disk rather than embedded — see docs/deploy.md.
type SPA struct {
	files  map[string]*asset
	shells []shell
}

type asset struct {
	body        []byte
	contentType string
	etag        string
	immutable   bool
}

// shell maps a path prefix to the document a navigation gets, and says whether a stranger may
// have it. One table, one answer to both questions.
type shell struct {
	prefix string
	doc    string
	public bool
}

// Explicit rather than mime.TypeByExtension, which reads /etc/mime.types — absent in a
// minimal container, which would serve every stylesheet as application/octet-stream.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8",
}

// NewSPA walks fsys once and copies every file into a map.
//
// A missing or empty directory is not fatal: the placeholder explains itself, the API still
// works, and tests need no bundle.
func NewSPA(fsys fs.FS) (*SPA, error) {
	s := &SPA{
		files: map[string]*asset{},
		shells: []shell{
			{prefix: "/login", doc: "login.html", public: true},
			// Public though it renders the same document: accepting an invitation is how a user
			// starts, and whoever is signed in already is not necessarily whoever the link was
			// sent to.
			{prefix: "/invite", doc: "login.html", public: true},
			{prefix: "/", doc: "index.html"},
		},
	}
	if fsys == nil {
		return s, nil
	}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		ct, ok := contentTypes[strings.ToLower(path.Ext(p))]
		if !ok {
			ct = "application/octet-stream"
		}
		s.files["/"+p] = &asset{
			body:        body,
			contentType: ct,
			etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
			// Vite content-hashes /assets, so the name changes when the bytes do.
			immutable: strings.HasPrefix(p, "assets/"),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// shellFor is the shell a navigation to p gets, and nil when a file sits at p.
//
// The nil is what keeps the bundle reachable to a stranger: a stylesheet is a file, so the
// login page can load its own assets without any of them being a route.
func (s *SPA) shellFor(p string) *shell {
	p = path.Clean(p)
	if s.files[p] != nil {
		return nil
	}
	// A path with an extension asked for a file, and there is not one. No route has an
	// extension, so this is a miss rather than a navigation, and ServeHTTP answers it with a 404.
	if path.Ext(p) != "" {
		return nil
	}
	for i := range s.shells {
		sh := &s.shells[i]
		if sh.prefix == "/" || p == sh.prefix || strings.HasPrefix(p, sh.prefix+"/") {
			return sh
		}
	}
	return nil
}

// ServeHTTP serves a file if there is one at that path, and otherwise the shell for it.
func (s *SPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a := s.files[path.Clean(r.URL.Path)]; a != nil {
		s.write(w, r, a)
		return
	}
	if sh := s.shellFor(r.URL.Path); sh != nil {
		if a := s.files["/"+sh.doc]; a != nil {
			s.write(w, r, a)
			return
		}
		// A navigation with no bundle behind it: the placeholder says which of the two
		// problems this is.
		s.placeholder(w)
		return
	}
	http.NotFound(w, r)
}

func (s *SPA) write(w http.ResponseWriter, r *http.Request, a *asset) {
	w.Header().Set("Content-Type", a.contentType)
	w.Header().Set("ETag", a.etag)
	if a.immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if match := r.Header.Get("If-None-Match"); match == a.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(a.body)
}

// placeholder names which problem this is: an unbuilt frontend and a broken server look
// identical from a blank page and send somebody to different logs.
func (s *SPA) placeholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(placeholderHTML))
}

const placeholderHTML = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>emguio</title>
<style>
  :root { color-scheme: light dark }
  body { font: 16px/1.5 system-ui, sans-serif; margin: 0; display: grid;
         place-items: center; min-height: 100dvh; padding: 1rem }
  main { max-width: 32rem }
  code { font-size: .9em }
</style>
<main>
  <h1>emguio</h1>
  <p>The frontend has not been built into this image, so there is no interface to show.
     The API is running: <code>GET /healthz</code> answers.</p>
</main>
`
