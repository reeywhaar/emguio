package message

import (
	"bufio"
	"bytes"
	"net/textproto"
	"net/url"
	"strings"
)

// Unsubscribe is how a message says to stop its mail: the web address in List-Unsubscribe (RFC
// 2369), and whether a request to it does it with nothing to read (RFC 8058). A mailto: address
// there is not used: unsubscribing is by web address only.
type Unsubscribe struct {
	// Link is its web address, empty when it gives none.
	Link string
	// OneClick is whether a POST to Link alone unsubscribes. Only over https.
	OneClick bool
}

// Any is whether it says how to unsubscribe, which is what makes a message a newsletter.
func (u Unsubscribe) Any() bool { return u.Link != "" }

// ListHeaders are the header fields UnsubscribeOf reads.
var ListHeaders = []string{"List-Unsubscribe", "List-Unsubscribe-Post"}

// UnsubscribeOf reads a message's List-Unsubscribe fields from its header: the first web address
// it names.
func UnsubscribeOf(header []byte) Unsubscribe {
	fields, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(bytes.TrimSpace(header), "\r\n\r\n"...)))).ReadMIMEHeader()
	if err != nil && len(fields) == 0 {
		return Unsubscribe{}
	}
	var u Unsubscribe
	for _, value := range fields.Values("List-Unsubscribe") {
		for rest := value; ; {
			start := strings.IndexByte(rest, '<')
			end := strings.IndexByte(rest, '>')
			if start < 0 || end < start {
				break
			}
			// A long address may be folded: whitespace inside the brackets is not part of it.
			raw := strings.Join(strings.Fields(rest[start+1:end]), "")
			rest = rest[end+1:]
			at, err := url.Parse(raw)
			if err != nil {
				continue
			}
			if scheme := strings.ToLower(at.Scheme); u.Link == "" && at.Host != "" && (scheme == "https" || scheme == "http") {
				u.Link = raw
			}
		}
	}
	post := strings.TrimSpace(fields.Get("List-Unsubscribe-Post"))
	u.OneClick = strings.EqualFold(post, "List-Unsubscribe=One-Click") && strings.HasPrefix(strings.ToLower(u.Link), "https://")
	return u
}
