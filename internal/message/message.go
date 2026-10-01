// Package message turns a message into what a reading pane shows: its text, its HTML made safe,
// and its parts. It fetches nothing; it is given the message as its server describes it, and the
// bytes of its text.
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

// Structure is a message as its server describes it, without the bytes of anything but its
// text: what a reading pane is made from, so opening a message never waits for its attachments.
type Structure struct {
	// Plain and HTML are its text, nil when it has none of that kind.
	Plain, HTML *Text
	// Leaves are every other part that is not a container: attachments, carried images.
	Leaves []Leaf
}

// Text is a text part's bytes as the server sends them.
type Text struct {
	Body     []byte
	Encoding string
	Charset  string
}

// Leaf is a part as the server describes it.
type Leaf struct {
	// Section is where the server holds it, as IMAP numbers parts: "2", "1.3".
	Section     string
	Type        string
	Params      map[string]string
	Disposition string
	// DispositionParams are the Content-Disposition's, where a filename usually is.
	DispositionParams map[string]string
	ContentID         string
	Encoding          string
	// Size is what the server holds, in its transfer encoding.
	Size int
}

// Part is one part of a message that is not its text: an attachment, or an image the HTML shows.
type Part struct {
	// Section is where the server holds it, and what it is fetched by, alone.
	Section   string
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
	// Held is how many images from elsewhere are held back until somebody asks.
	Held  int
	Parts []Part
}

// Options say where the HTML's images come from.
type Options struct {
	// PartURL is where the reading pane fetches the part at section, for an image the message
	// carries.
	PartURL func(section string) string
	// Proxy is where it fetches a remote image through.
	Proxy func(remote string) string
	// Hold keeps remote images back until somebody asks: a blank image in place of each, and
	// the proxy's address for it in data-src. Without it they load at once, through the proxy.
	Hold bool
}

// ErrUnreadable is a part enmime could make nothing of.
var ErrUnreadable = errors.New("message: unreadable")

// Show makes a message ready to read.
func Show(st Structure, opts Options) *Read {
	out := &Read{}
	var rich string
	if st.Plain != nil {
		out.Text = clean(decodeText(st.Plain.Body, st.Plain.Encoding, st.Plain.Charset))
	}
	if st.HTML != nil {
		rich = decodeText(st.HTML.Body, st.HTML.Encoding, st.HTML.Charset)
	}
	if out.Text != "" || rich == "" {
		out.Preview = Preview(out.Text)
	} else {
		out.Preview = Preview(clean(htmlText(rich)))
	}

	cids := map[string]string{}
	for _, l := range st.Leaves {
		if cid := strings.Trim(l.ContentID, "<> "); cid != "" {
			cids[cid] = l.Section
		}
	}
	if rich != "" {
		out.HTML, out.Held = rewrite(policy.Sanitize(rich), opts, cids)
	}
	shown := map[string]bool{}
	for cid, at := range cids {
		if out.HTML != "" && strings.Contains(rich, "cid:"+cid) {
			shown[at] = true
		}
	}
	for _, l := range st.Leaves {
		out.Parts = append(out.Parts, Part{
			Section:   l.Section,
			Name:      leafName(l),
			Type:      strings.ToLower(l.Type),
			Size:      decodedSize(l),
			ContentID: strings.Trim(l.ContentID, "<> "),
			Listed:    !shown[l.Section],
		})
	}
	return out
}

// leafName is what a part is called: its filename, or the name its type gives it, or where it is.
func leafName(l Leaf) string {
	if name := clean(Param(l.DispositionParams, "filename")); name != "" {
		return name
	}
	if name := clean(Param(l.Params, "name")); name != "" {
		return name
	}
	if strings.EqualFold(l.Type, "message/rfc822") {
		return "message-" + l.Section + ".eml"
	}
	return "part-" + l.Section
}

// decodedSize is about how large a part is once its transfer encoding is undone: base64 carries
// 57 bytes on every line of 78.
func decodedSize(l Leaf) int {
	if strings.EqualFold(l.Encoding, "base64") {
		return l.Size * 57 / 78
	}
	return l.Size
}

// DecodePart is one part fetched alone — its MIME header and its body, as the server sends
// them — decoded.
func DecodePart(header, body []byte, at string) (*Part, []byte, error) {
	p, err := enmime.ReadParts(bytes.NewReader(append(append([]byte(nil), header...), body...)))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	return &Part{
		Section:   at,
		Name:      partName(p, at),
		Type:      strings.ToLower(p.ContentType),
		Size:      len(p.Content),
		ContentID: p.ContentID,
	}, p.Content, nil
}

func partName(p *enmime.Part, at string) string {
	if name := clean(p.FileName); name != "" {
		return name
	}
	return "part-" + at
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
// image at its part, and a remote one at the proxy, or, held, at a blank image with the proxy's
// address in data-src for the pane to swap in. It reports how many were held.
func rewrite(in string, opts Options, cids map[string]string) (string, int) {
	z := html.NewTokenizer(strings.NewReader(in))
	var out strings.Builder
	held := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if !errors.Is(z.Err(), io.EOF) {
				out.Write(z.Raw())
			}
			return out.String(), held
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
			if a.Key == "data-src" {
				continue
			}
			if a.Key != "src" {
				attrs = append(attrs, a)
				continue
			}
			src, later, kept := imageSource(a.Val, opts, cids)
			if kept {
				held++
			}
			if src != "" {
				attrs = append(attrs, html.Attribute{Key: "src", Val: src})
			}
			if later != "" {
				attrs = append(attrs, html.Attribute{Key: "data-src", Val: later})
			}
		}
		t.Attr = attrs
		out.WriteString(t.String())
	}
}

// imageSource is where an image is fetched from now, empty for nowhere, and whether it is held
// back; for a held one, where it is fetched from once somebody asks.
func imageSource(src string, opts Options, cids map[string]string) (now, later string, held bool) {
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil {
		return "", "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "data":
		return src, "", false
	case "cid":
		cid, err := url.PathUnescape(u.Opaque)
		if err != nil {
			return "", "", false
		}
		if at, ok := cids[strings.Trim(cid, "<>")]; ok && opts.PartURL != nil {
			return opts.PartURL(at), "", false
		}
		return "", "", false
	case "http", "https":
		switch {
		case opts.Proxy == nil:
			return blank, "", true
		case opts.Hold:
			return blank, opts.Proxy(u.String()), true
		}
		return opts.Proxy(u.String()), "", false
	}
	return "", "", false
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
