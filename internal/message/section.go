package message

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/quotedprintable"
	"regexp"
	"strings"

	"github.com/emersion/go-message/charset"
	"golang.org/x/net/html"
)

// SectionPreview is a list's preview from the first bytes of one text part, as a sync fetches
// them: cut off wherever the fetch stopped, still in its transfer encoding and its charset, and
// HTML when the part is.
//
// Cut off is the normal case, so every decoder here takes what it can and stops quietly.
func SectionPreview(content []byte, encoding, declared string, isHTML bool) string {
	var decoded []byte
	switch strings.ToLower(encoding) {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, content)
		clean = clean[:len(clean)-len(clean)%4]
		decoded = make([]byte, base64.StdEncoding.DecodedLen(len(clean)))
		n, _ := base64.StdEncoding.Decode(decoded, clean)
		decoded = decoded[:n]
	case "quoted-printable":
		decoded, _ = io.ReadAll(quotedprintable.NewReader(bytes.NewReader(content)))
	default:
		decoded = content
	}

	text := string(decoded)
	if declared != "" && !strings.EqualFold(declared, "utf-8") && !strings.EqualFold(declared, "us-ascii") {
		if r, err := charset.Reader(declared, bytes.NewReader(decoded)); err == nil {
			if b, err := io.ReadAll(r); err == nil || len(b) > 0 {
				text = string(b)
			}
		}
	}
	if isHTML {
		text = htmlText(text)
	}
	return Preview(clean(text))
}

// unfinishedTag is a tag the fetch cut in half, which would otherwise be read as text.
var unfinishedTag = regexp.MustCompile(`<[^>]*$`)

// hidden are elements whose text is never shown.
var hidden = map[string]bool{"head": true, "title": true, "style": true, "script": true, "noscript": true, "template": true}

// htmlText is the words of an HTML fragment: entities decoded, what is never shown left out, and
// a space wherever a tag stood, so two cells or two paragraphs do not run together.
func htmlText(s string) string {
	z := html.NewTokenizer(strings.NewReader(unfinishedTag.ReplaceAllString(s, "")))
	var out strings.Builder
	depth := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out.String()
		case html.StartTagToken:
			if name, _ := z.TagName(); hidden[string(name)] {
				depth++
			}
			out.WriteByte(' ')
		case html.EndTagToken:
			if name, _ := z.TagName(); hidden[string(name)] && depth > 0 {
				depth--
			}
			out.WriteByte(' ')
		case html.SelfClosingTagToken:
			out.WriteByte(' ')
		case html.TextToken:
			if depth == 0 {
				out.Write(z.Text())
			}
		}
	}
}
