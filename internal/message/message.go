// Package message turns a raw message into what a reading pane shows: its text, its HTML made
// safe, and its parts. It fetches nothing; what it is given is all there is.
//
// HTML from a message is written by a stranger. It is sanitized here, and the browser still
// shows it only in a sandboxed frame — see docs/reading.md.
package message

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jhillyerd/enmime/v2"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// Part is one part of a message that is not its text: an attachment, or an image the HTML shows.
type Part struct {
	Index     int
	Name      string
	Type      string
	Size      int
	ContentID string
	// Listed is false for an image the HTML already shows in place, which a list of attachments
	// would only repeat.
	Listed bool
}

// Read is a message ready to show.
type Read struct {
	Text string
	// Preview is the start of the text, on one line, for a list.
	Preview string
	// HTML is sanitized, and empty when the message has none.
	HTML string
	// Remote is how many images the HTML asks for from elsewhere.
	Remote int
	Parts  []Part
}

// Options say where the HTML's images come from.
type Options struct {
	// PartURL is where the reading pane fetches part i, for an image the message carries.
	PartURL func(i int) string
	// Proxy is where it fetches a remote image through; nil blocks every one.
	Proxy func(remote string) string
}

// ErrUnreadable is a message enmime could make nothing of.
var ErrUnreadable = errors.New("message: unreadable")

// Parse reads a raw message.
func Parse(raw []byte, opts Options) (*Read, error) {
	env, parts, err := read(raw)
	if err != nil {
		return nil, err
	}
	out := &Read{Text: clean(env.Text)}
	// A message with no plain text of its own has text made from its HTML, which marks headings
	// and the like with asterisks. Its preview is read from the HTML's words instead.
	if env.HTML != "" && !hasPlain(env.Root) {
		out.Preview = Preview(clean(htmlText(env.HTML)))
	} else {
		out.Preview = Preview(out.Text)
	}

	cids := map[string]int{}
	for i, p := range parts {
		if p.ContentID != "" {
			cids[p.ContentID] = i
		}
	}
	if env.HTML != "" {
		out.HTML, out.Remote = rewrite(policy.Sanitize(env.HTML), opts, cids)
	}
	shown := map[int]bool{}
	for cid, i := range cids {
		if out.HTML != "" && strings.Contains(env.HTML, "cid:"+cid) {
			shown[i] = true
		}
	}
	for i, p := range parts {
		out.Parts = append(out.Parts, Part{
			Index:     i,
			Name:      partName(p, i),
			Type:      strings.ToLower(p.ContentType),
			Size:      len(p.Content),
			ContentID: p.ContentID,
			Listed:    !shown[i],
		})
	}
	return out, nil
}

// PartOf is part i of a raw message and its bytes, numbered as Parse numbers them.
func PartOf(raw []byte, i int) (*Part, []byte, error) {
	_, parts, err := read(raw)
	if err != nil {
		return nil, nil, err
	}
	if i < 0 || i >= len(parts) {
		return nil, nil, fmt.Errorf("message: no part %d", i)
	}
	p := parts[i]
	return &Part{
		Index:     i,
		Name:      partName(p, i),
		Type:      strings.ToLower(p.ContentType),
		Size:      len(p.Content),
		ContentID: p.ContentID,
	}, p.Content, nil
}

// read parses, and numbers the parts that are not text: attachments, then inline parts, then the
// rest that are not containers. The same message is always numbered the same way.
func read(raw []byte) (*enmime.Envelope, []*enmime.Part, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	var parts []*enmime.Part
	for _, group := range [][]*enmime.Part{env.Attachments, env.Inlines, env.OtherParts} {
		for _, p := range group {
			if strings.HasPrefix(strings.ToLower(p.ContentType), "multipart/") {
				continue
			}
			parts = append(parts, p)
		}
	}
	return env, parts, nil
}

// hasPlain reports whether a message carries text/plain of its own, outside its attachments.
func hasPlain(p *enmime.Part) bool {
	for ; p != nil; p = p.NextSibling {
		if strings.EqualFold(p.ContentType, "text/plain") && !strings.EqualFold(p.Disposition, "attachment") {
			return true
		}
		if hasPlain(p.FirstChild) {
			return true
		}
	}
	return false
}

func partName(p *enmime.Part, i int) string {
	if name := clean(p.FileName); name != "" {
		return name
	}
	return fmt.Sprintf("part-%d", i+1)
}

// policy is what HTML from a message may keep: text formatting, tables and the presentational
// attributes newsletters still lay themselves out with, inline styles for properties that cannot
// load anything, links, and images. No script, no forms, no frames, no style sheets.
var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowURLSchemes("http", "https", "mailto", "cid")
	p.AllowDataURIImages()
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowElements("font", "center")
	p.AllowAttrs("color", "face", "size").OnElements("font")
	p.AllowAttrs("align", "valign", "width", "height", "bgcolor", "border",
		"cellpadding", "cellspacing").Globally()
	// None of these can name a URL, so none can load anything.
	p.AllowStyles(
		"color", "background-color",
		"font", "font-family", "font-size", "font-style", "font-weight", "font-variant",
		"text-align", "text-decoration", "text-indent", "text-transform", "line-height",
		"letter-spacing", "word-spacing", "white-space", "vertical-align", "direction",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border", "border-top", "border-right", "border-bottom", "border-left",
		"border-color", "border-style", "border-width", "border-radius",
		"border-collapse", "border-spacing",
		"width", "height", "max-width", "min-width", "max-height", "min-height",
		"display", "float", "clear", "table-layout", "list-style-type", "opacity",
	).Globally()
	return p
}()

// rewrite points the sanitized HTML's images where the reading pane can fetch them: a carried
// image at its part, a remote one at the proxy or nowhere. It reports how many were remote.
func rewrite(in string, opts Options, cids map[string]int) (string, int) {
	z := html.NewTokenizer(strings.NewReader(in))
	var out strings.Builder
	remote := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if !errors.Is(z.Err(), io.EOF) {
				out.Write(z.Raw())
			}
			return out.String(), remote
		}
		raw := string(z.Raw())
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			out.WriteString(raw)
			continue
		}
		t := z.Token()
		if t.Data != "img" {
			out.WriteString(raw)
			continue
		}
		var attrs []html.Attribute
		for _, a := range t.Attr {
			if a.Key != "src" {
				attrs = append(attrs, a)
				continue
			}
			src, counted := imageSource(a.Val, opts, cids)
			if counted {
				remote++
			}
			if src != "" {
				attrs = append(attrs, html.Attribute{Key: "src", Val: src})
			}
		}
		t.Attr = attrs
		out.WriteString(t.String())
	}
}

// imageSource is where an image is fetched from now, empty for nowhere, and whether it was a
// remote one.
func imageSource(src string, opts Options, cids map[string]int) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "data":
		return src, false
	case "cid":
		cid, err := url.PathUnescape(u.Opaque)
		if err != nil {
			return "", false
		}
		if i, ok := cids[strings.Trim(cid, "<>")]; ok && opts.PartURL != nil {
			return opts.PartURL(i), false
		}
		return "", false
	case "http", "https":
		if opts.Proxy != nil {
			return opts.Proxy(u.String()), true
		}
		return blank, true
	}
	return "", false
}

// blank stands where a remote image is held back: an image that draws nothing, so the layout the
// sender's width and height give it holds, without a browser's broken-image box in every slot.
const blank = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"

// Preview is the start of a message's text, on one line: what a list shows under the subject.
// Quoted lines are skipped, because the start of a reply quoting the whole thread is the thread.
func Preview(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ">") {
			continue
		}
		kept = append(kept, line)
		if len(kept) > 8 {
			break
		}
	}
	return cut(strings.Join(strings.Fields(strings.Join(kept, " ")), " "), 160)
}

// clean is text made fit to store and show: valid UTF-8, without the NULs and other controls a
// broken message carries, line breaks and tabs kept.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}
