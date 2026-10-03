package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"emguio/internal/config"
)

// routeLine is a route as docs/api.md writes one: its method and path at the start of a line.
var routeLine = regexp.MustCompile(`(?m)^(GET|POST|PUT|PATCH|DELETE)\s+(/api/\S+)`)

// normalise makes {id} and {mailbox} comparable however each side spells the parameter.
func normalise(path string) string {
	return regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(path, "{}")
}

func apiDoc(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("../../docs/api.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

// A stale reference costs a reader the time to find out, so the one thing that actually rots —
// an endpoint added, renamed or removed — is pinned here, both ways. The prose is a person's job.
func TestTheRouteTableAndTheDocsAgree(t *testing.T) {
	s := newServer(t, nil)
	registered := map[string]bool{}
	for _, route := range s.Routes() {
		method, path, _ := strings.Cut(route, " ")
		registered[method+" "+normalise(path)] = true
	}
	documented := map[string]bool{}
	for _, line := range routeLine.FindAllStringSubmatch(apiDoc(t), -1) {
		route := line[1] + " " + normalise(line[2])
		documented[route] = true
		if !registered[route] {
			t.Errorf("docs/api.md documents %s, which is not a route", route)
		}
	}
	var missing []string
	for route := range registered {
		if !documented[route] {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)
	for _, route := range missing {
		t.Errorf("%s is a route and not in docs/api.md", route)
	}
}

// A caller that has not been told a code cannot act on it, and one told of a code the server
// never sends handles a branch that never runs.
func TestTheDocumentedCodesAreTheOnesSent(t *testing.T) {
	text := apiDoc(t)
	at := strings.Index(text, "## Errors")
	if at < 0 {
		t.Fatal("docs/api.md has no Errors section")
	}
	documented := map[string]bool{}
	for _, row := range regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|").FindAllStringSubmatch(text[at:], -1) {
		documented[row[1]] = true
	}

	// Where the constants are declared does not count as sending one.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := regexp.MustCompile(`(?m)^\s*(Code\w+)\s*=\s*"([a-z_]+)"`)
	codes := map[string]string{}
	var sources []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if f == "errors.go" {
			for _, m := range declared.FindAllStringSubmatch(string(body), -1) {
				codes[m[1]] = m[2]
			}
			continue
		}
		sources = append(sources, string(body))
	}
	all := strings.Join(sources, "\n")
	for name, code := range codes {
		sent := strings.Contains(all, name)
		switch {
		case sent && !documented[code]:
			t.Errorf("code %q is sent and not in docs/api.md", code)
		case !sent && documented[code]:
			t.Errorf("docs/api.md documents code %q, which nothing sends", code)
		}
		delete(documented, code)
	}
	for code := range documented {
		t.Errorf("docs/api.md documents code %q, which is not a code", code)
	}
}

// The reference is anybody's, signed in or not, with the instance's own address in its examples.
func TestTheReferenceIsServedToAnybody(t *testing.T) {
	_, st := newServerStore(t, nil)
	u, _ := url.Parse("https://mail.example.com")
	docs := NewDocs(fstest.MapFS{
		"docs.html": {Data: []byte("<p>curl https://emguio.example.com/api/auth/me</p>")},
		"docs.md":   {Data: []byte("curl https://emguio.example.com/api/auth/me")},
	}, nil, u.String())
	s := New(&config.Config{PublicURL: u, Secure: true}, slog.New(slog.DiscardHandler), st, nil, docs, nil)
	for path, want := range map[string]string{
		"/docs":     "<p>curl https://mail.example.com/api/auth/me</p>",
		"/docs.md":  "curl https://mail.example.com/api/auth/me",
		"/llms.txt": "https://mail.example.com/docs.md",
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		body, _ := io.ReadAll(rec.Body)
		if rec.Code != http.StatusOK || !strings.Contains(string(body), want) {
			t.Errorf("%s = %d %q", path, rec.Code, body)
		}
	}
	// Without a bundle, the markdown is what there is.
	bare := New(&config.Config{PublicURL: u}, slog.New(slog.DiscardHandler), st, nil,
		NewDocs(nil, fstest.MapFS{"api.md": {Data: []byte("# emguio API")}}, ""), nil)
	rec := httptest.NewRecorder()
	bare.ServeHTTP(rec, httptest.NewRequest("GET", "/docs", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Errorf("/docs without a bundle = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}
