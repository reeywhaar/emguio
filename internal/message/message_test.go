package message

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// mail is a message with \n line ends, turned into the \r\n a server sends.
func mail(s string) []byte { return []byte(strings.ReplaceAll(s, "\n", "\r\n")) }

func htmlMail(body string) []byte {
	return mail("From: a@example.com\nSubject: hi\nMIME-Version: 1.0\nContent-Type: text/html; charset=utf-8\n\n" + body + "\n")
}

var parts = Options{PartURL: func(i int) string { return fmt.Sprintf("/part/%d", i) }}

func parse(t *testing.T, raw []byte, opts Options) *Read {
	t.Helper()
	r, err := Parse(raw, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The frame runs no script either, but the sanitizer is the first wall and must stand on its own.
func TestNothingThatRunsSurvives(t *testing.T) {
	r := parse(t, htmlMail(`<p onclick="steal()">Hi</p>
<script>steal()</script>
<a href="javascript:steal()">click</a>
<img src="x" onerror="steal()">
<iframe src="https://evil.example"></iframe>
<form action="https://evil.example"><input name="password"></form>
<svg><script>steal()</script></svg>
<object data="https://evil.example/x.swf"></object>
<a href="https://example.com">fine</a>`), parts)

	for _, bad := range []string{"<script", "onclick", "onerror", "javascript:", "<iframe", "<form", "<input", "<object", "steal()"} {
		if strings.Contains(r.HTML, bad) {
			t.Errorf("%q survived:\n%s", bad, r.HTML)
		}
	}
	if !strings.Contains(r.HTML, `href="https://example.com"`) || !strings.Contains(r.HTML, "fine") {
		t.Errorf("an ordinary link was lost:\n%s", r.HTML)
	}
}

// A style that names a URL is a request to somewhere, made without asking.
func TestNoStyleCanLoadAnything(t *testing.T) {
	r := parse(t, htmlMail(`<div style="background-image: url(https://track.example/p.gif); color: red">a</div>
<style>body { background: url(https://track.example/q.gif) }</style>
<table background="https://track.example/t.gif"><tr><td>b</td></tr></table>
<link rel="stylesheet" href="https://track.example/s.css">`), parts)

	if strings.Contains(r.HTML, "track.example") {
		t.Errorf("a remote load survived:\n%s", r.HTML)
	}
	if !strings.Contains(r.HTML, "color: red") {
		t.Errorf("a harmless style was lost:\n%s", r.HTML)
	}
}

func TestLinksOpenElsewhereAndSayNothingAboutWhereFrom(t *testing.T) {
	r := parse(t, htmlMail(`<a href="https://example.com/page">x</a>`), parts)
	if !strings.Contains(r.HTML, `target="_blank"`) || !strings.Contains(r.HTML, "noreferrer") {
		t.Errorf("link = %s", r.HTML)
	}
}

// Remote images tell the sender the message was opened, and from where.
func TestRemoteImagesAreBlockedUnlessAskedFor(t *testing.T) {
	raw := htmlMail(`<img src="https://track.example/pixel.gif" alt="logo"><img src="http://cdn.example/b.png">`)

	blocked := parse(t, raw, parts)
	if blocked.Remote != 2 || strings.Contains(blocked.HTML, "example/") {
		t.Errorf("blocked: remote = %d\n%s", blocked.Remote, blocked.HTML)
	}
	if strings.Count(blocked.HTML, `src="data:image/gif`) != 2 {
		t.Errorf("a blocked image does not hold its place with a blank one:\n%s", blocked.HTML)
	}
	if !strings.Contains(blocked.HTML, `alt="logo"`) {
		t.Errorf("the blocked image lost its alt text:\n%s", blocked.HTML)
	}

	proxied := parse(t, raw, Options{Proxy: func(u string) string { return "/proxy?u=" + u }})
	if !strings.Contains(proxied.HTML, `src="/proxy?u=https://track.example/pixel.gif"`) {
		t.Errorf("proxied:\n%s", proxied.HTML)
	}
}

func TestACarriedImageIsShownInPlaceAndNotListed(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))
	raw := mail(`From: a@example.com
Subject: logo
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=outer

--outer
Content-Type: multipart/related; boundary=inner

--inner
Content-Type: text/html; charset=utf-8

<p>Hi <img src="cid:logo@example"></p>
--inner
Content-Type: image/png
Content-ID: <logo@example>
Content-Disposition: inline; filename=logo.png
Content-Transfer-Encoding: base64

` + png + `
--inner--
--outer
Content-Type: application/pdf
Content-Disposition: attachment; filename="report.pdf"
Content-Transfer-Encoding: base64

JVBERi0=
--outer--
`)
	r := parse(t, raw, parts)

	var logo, report *Part
	for i := range r.Parts {
		switch r.Parts[i].Name {
		case "logo.png":
			logo = &r.Parts[i]
		case "report.pdf":
			report = &r.Parts[i]
		}
	}
	if logo == nil || report == nil {
		t.Fatalf("parts = %+v", r.Parts)
	}
	if !strings.Contains(r.HTML, fmt.Sprintf(`src="/part/%d"`, logo.Index)) {
		t.Errorf("the cid image does not point at its part:\n%s", r.HTML)
	}
	if logo.Listed || !report.Listed {
		t.Errorf("listed: logo %v, report %v", logo.Listed, report.Listed)
	}
	if report.Type != "application/pdf" || report.Size != 5 {
		t.Errorf("report = %+v", report)
	}

	part, content, err := PartOf(raw, report.Index)
	if err != nil || part.Name != "report.pdf" || string(content) != "%PDF-" {
		t.Errorf("PartOf = %+v, %q, %v", part, content, err)
	}
}

func TestABodyIsReadInItsCharset(t *testing.T) {
	// "Привет" in koi8-r, quoted-printable.
	raw := mail("From: a@example.com\nSubject: s\nMIME-Version: 1.0\nContent-Type: text/plain; charset=koi8-r\nContent-Transfer-Encoding: quoted-printable\n\n=F0=D2=C9=D7=C5=D4\n")
	if r := parse(t, raw, parts); strings.TrimSpace(r.Text) != "Привет" {
		t.Errorf("text = %q", r.Text)
	}
}

func TestAnHTMLOnlyMessageStillHasText(t *testing.T) {
	r := parse(t, htmlMail(`<h1>News</h1><p>Hello <b>there</b></p>`), parts)
	if !strings.Contains(r.Text, "Hello") || r.HTML == "" {
		t.Errorf("text = %q, html = %q", r.Text, r.HTML)
	}
	// The text made from HTML marks a heading with asterisks; a preview has none of that.
	if r.Preview != "News Hello there" {
		t.Errorf("preview = %q", r.Preview)
	}
}

func TestAPlainMessageHasNoHTML(t *testing.T) {
	r := parse(t, mail("From: a@example.com\nSubject: s\n\nJust text.\n"), parts)
	if r.HTML != "" || strings.TrimSpace(r.Text) != "Just text." {
		t.Errorf("text = %q, html = %q", r.Text, r.HTML)
	}
}

func TestAPreviewSkipsQuotesAndFitsOnOneLine(t *testing.T) {
	got := Preview("Sounds good.\n\nSee you\tthere.\n\n> On Monday you wrote:\n> long quote\n")
	if got != "Sounds good. See you there." {
		t.Errorf("preview = %q", got)
	}
	if long := Preview(strings.Repeat("word ", 100)); len([]rune(long)) > 161 || !strings.HasSuffix(long, "…") {
		t.Errorf("long preview = %q", long)
	}
}

// What a sync fetches is the first bytes of a part, cut wherever the fetch stopped.
func TestASectionPreviewReadsWhatWasCutOff(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("Hello from base64, and more"))
	if got := SectionPreview([]byte(b64[:17]+"\r\n"+b64[17:22]), "base64", "utf-8", false); !strings.HasPrefix(got, "Hello from") {
		t.Errorf("base64 = %q", got)
	}
	if got := SectionPreview([]byte("caf=C3=A9 au la=\r\nit, cut =C3"), "quoted-printable", "utf-8", false); !strings.HasPrefix(got, "café au lait, cut") {
		t.Errorf("quoted-printable = %q", got)
	}
	if got := SectionPreview([]byte{0xF0, 0xD2, 0xC9, 0xD7, 0xC5, 0xD4}, "8bit", "koi8-r", false); got != "Привет" {
		t.Errorf("koi8-r = %q", got)
	}
	if got := SectionPreview([]byte(`<html><head><title>T</title><style>p{color:red}</style></head><body><p>Hi&nbsp;<b>you</b></p><p>there</p><a hr`), "7bit", "", true); got != "Hi you there" {
		t.Errorf("html = %q", got)
	}
}
