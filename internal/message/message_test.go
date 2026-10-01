package message

import (
	"encoding/base64"
	"strings"
	"testing"
)

// mail is MIME with \n line ends, turned into the \r\n a server sends.
func mail(s string) []byte { return []byte(strings.ReplaceAll(s, "\n", "\r\n")) }

// htmlOnly is a message whose one text is HTML.
func htmlOnly(body string) Structure {
	return Structure{HTML: &Text{Body: []byte(body), Charset: "utf-8"}}
}

var parts = Options{PartURL: func(at string) string { return "/part/" + at }}

// The frame runs no script either, but the sanitizer is the first wall and must stand on its own.
func TestNothingThatRunsSurvives(t *testing.T) {
	r := Show(htmlOnly(`<p onclick="steal()">Hi</p>
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
	r := Show(htmlOnly(`<div style="background-image: url(https://track.example/p.gif); color: red">a</div>
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
	r := Show(htmlOnly(`<a href="https://example.com/page">x</a>`), parts)
	if !strings.Contains(r.HTML, `target="_blank"`) || !strings.Contains(r.HTML, "noreferrer") {
		t.Errorf("link = %s", r.HTML)
	}
}

// Remote images tell the sender the message was opened, and from where. Each holds its place
// with a blank image, and carries the proxy's address for the pane to swap in when asked.
func TestRemoteImagesWaitToBeAskedFor(t *testing.T) {
	raw := htmlOnly(`<img src="https://track.example/pixel.gif" alt="logo"><img src="http://cdn.example/b.png" data-src="https://evil.example/x.gif">`)

	r := Show(raw, Options{Proxy: func(u string) string { return "/proxy?u=" + u }})
	if r.Remote != 2 || strings.Contains(r.HTML, `src="http`) || strings.Contains(r.HTML, "evil.example") {
		t.Errorf("remote = %d\n%s", r.Remote, r.HTML)
	}
	if strings.Count(r.HTML, `src="data:image/gif`) != 2 {
		t.Errorf("a waiting image does not hold its place with a blank one:\n%s", r.HTML)
	}
	if !strings.Contains(r.HTML, `data-src="/proxy?u=https://track.example/pixel.gif"`) ||
		!strings.Contains(r.HTML, `data-src="/proxy?u=http://cdn.example/b.png"`) {
		t.Errorf("the proxy's addresses are not there to swap in:\n%s", r.HTML)
	}
	if !strings.Contains(r.HTML, `alt="logo"`) {
		t.Errorf("the image lost its alt text:\n%s", r.HTML)
	}

	blocked := Show(raw, parts)
	if blocked.Remote != 2 || strings.Contains(blocked.HTML, "data-src") {
		t.Errorf("without a proxy: remote = %d\n%s", blocked.Remote, blocked.HTML)
	}
}

func TestACarriedImageIsShownInPlaceAndNotListed(t *testing.T) {
	r := Show(Structure{
		HTML: &Text{Body: []byte(`<p>Hi <img src="cid:logo@example"></p>`)},
		Leaves: []Leaf{
			{Section: "1.2", Type: "image/png", ContentID: "<logo@example>", Disposition: "inline",
				DispositionParams: map[string]string{"filename": "logo.png"}, Encoding: "base64", Size: 78},
			{Section: "2", Type: "application/pdf", Disposition: "attachment",
				DispositionParams: map[string]string{"filename": "report.pdf"}, Encoding: "base64", Size: 7800},
		},
	}, parts)

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
	if !strings.Contains(r.HTML, `src="/part/1.2"`) {
		t.Errorf("the cid image does not point at its part:\n%s", r.HTML)
	}
	if logo.Listed || !report.Listed {
		t.Errorf("listed: logo %v, report %v", logo.Listed, report.Listed)
	}
	// What the server holds is base64; what somebody downloads is about three quarters of it.
	if report.Type != "application/pdf" || report.Size != 5700 || report.Section != "2" {
		t.Errorf("report = %+v", report)
	}
}

// A sender's filename arrives as they wrote it, however they wrote it.
func TestAPartIsNamedAsItsSenderNamedIt(t *testing.T) {
	for want, l := range map[string]Leaf{
		"report.pdf": {DispositionParams: map[string]string{"filename": "report.pdf"}},
		"Отчёт.pdf":  {DispositionParams: map[string]string{"filename*": "utf-8''%D0%9E%D1%82%D1%87%D1%91%D1%82.pdf"}},
		"Отчёт за 2026.pdf": {DispositionParams: map[string]string{
			"filename*0*": "utf-8''%D0%9E%D1%82%D1%87%D1%91%D1%82",
			"filename*1":  " за 2026",
			"filename*2*": ".pdf",
		}},
		"scan.jpg":      {Params: map[string]string{"name": "scan.jpg"}},
		"part-3":        {Section: "3"},
		"message-2.eml": {Section: "2", Type: "message/rfc822"},
	} {
		if got := leafName(l); got != want {
			t.Errorf("%+v named %q, want %q", l, got, want)
		}
	}
}

// A part fetched alone is its MIME header and its body as the server sends them.
func TestAPartFetchedAloneIsDecoded(t *testing.T) {
	head := mail("Content-Type: application/pdf\nContent-Disposition: attachment; filename=\"report.pdf\"\nContent-Transfer-Encoding: base64\n\n")
	part, content, err := DecodePart(head, mail("JVBERi0=\n"), "2")
	if err != nil || part.Name != "report.pdf" || part.Type != "application/pdf" || string(content) != "%PDF-" {
		t.Errorf("DecodePart = %+v, %q, %v", part, content, err)
	}
	if part, _, _ := DecodePart(mail("Content-Type: image/png\n\n"), []byte("x"), "1.3"); part.Name != "part-1.3" {
		t.Errorf("an unnamed part = %q", part.Name)
	}
}

func TestABodyIsReadInItsCharset(t *testing.T) {
	r := Show(Structure{Plain: &Text{Body: []byte("0J/RgNC40LLQtdGCDQo="), Encoding: "base64", Charset: "utf-8"}}, parts)
	if strings.TrimSpace(r.Text) != "Привет" || r.Preview != "Привет" {
		t.Errorf("text = %q, preview = %q", r.Text, r.Preview)
	}
	// Not every sender writes UTF-8 yet: GB2312 is common in Chinese mail.
	r = Show(Structure{Plain: &Text{Body: []byte{0xC4, 0xE3, 0xBA, 0xC3}, Charset: "gb2312"}}, parts)
	if r.Text != "你好" {
		t.Errorf("gb2312 = %q", r.Text)
	}
}

func TestAnHTMLOnlyMessageIsPreviewedFromItsWords(t *testing.T) {
	r := Show(htmlOnly(`<h1>News</h1><p>Hello <b>there</b></p>`), parts)
	if r.HTML == "" || r.Preview != "News Hello there" {
		t.Errorf("html = %q, preview = %q", r.HTML, r.Preview)
	}
}

func TestAPlainMessageHasNoHTML(t *testing.T) {
	r := Show(Structure{Plain: &Text{Body: []byte("Just text.\r\n")}}, parts)
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
	if got := SectionPreview([]byte{0xC4, 0xE3, 0xBA, 0xC3}, "8bit", "gb2312", false); got != "你好" {
		t.Errorf("gb2312 = %q", got)
	}
	if got := SectionPreview([]byte(`<html><head><title>T</title><style>p{color:red}</style></head><body><p>Hi&nbsp;<b>you</b></p><p>there</p><a hr`), "7bit", "", true); got != "Hi you there" {
		t.Errorf("html = %q", got)
	}
}
