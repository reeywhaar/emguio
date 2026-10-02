package mirror

import (
	"strings"
	"unicode"

	"github.com/emersion/go-imap/v2"
)

// Criteria is what somebody typed into the search field, as IMAP SEARCH criteria, or nil when it
// holds nothing to search for. Every word has to be in the message, its headers or its text;
// from:, to: and subject: narrow a word to that header, to: taking Cc too; is:unread, is:read and
// is:starred are the flags. Quotes keep words together, after an operator too. See
// docs/reading.md.
func Criteria(q string) *imap.SearchCriteria {
	var c imap.SearchCriteria
	found := false
	for _, w := range words(q) {
		found = true
		key, value, ok := strings.Cut(w, ":")
		if ok && value != "" {
			switch strings.ToLower(key) {
			case "from":
				c.Header = append(c.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: value})
				continue
			case "subject":
				c.Header = append(c.Header, imap.SearchCriteriaHeaderField{Key: "Subject", Value: value})
				continue
			case "to":
				c.Or = append(c.Or, [2]imap.SearchCriteria{
					{Header: []imap.SearchCriteriaHeaderField{{Key: "To", Value: value}}},
					{Header: []imap.SearchCriteriaHeaderField{{Key: "Cc", Value: value}}},
				})
				continue
			case "is":
				switch strings.ToLower(value) {
				case "unread":
					c.NotFlag = append(c.NotFlag, imap.FlagSeen)
					continue
				case "read":
					c.Flag = append(c.Flag, imap.FlagSeen)
					continue
				case "starred":
					c.Flag = append(c.Flag, imap.FlagFlagged)
					continue
				}
			}
		}
		c.Text = append(c.Text, w)
	}
	if !found {
		return nil
	}
	return &c
}

// words splits a query at spaces outside quotes, and drops the quotes.
func words(q string) []string {
	var (
		out    []string
		w      strings.Builder
		quoted bool
	)
	flush := func() {
		if w.Len() > 0 {
			out = append(out, w.String())
			w.Reset()
		}
	}
	for _, r := range q {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			w.WriteRune(r)
		}
	}
	flush()
	return out
}
