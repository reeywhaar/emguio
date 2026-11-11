package mirror

import (
	"testing"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/store"
)

// A flag emguio knows says what a folder is for; any other that is not about the tree makes it
// the server's own. A folder of the user's carries none of those.
func TestAFlagEmguioDoesNotKnowMakesAFolderTheServers(t *testing.T) {
	for _, c := range []struct {
		attrs []imap.MailboxAttr
		want  string
	}{
		{[]imap.MailboxAttr{imap.MailboxAttrHasNoChildren, `\Sent`}, store.UseSent},
		{[]imap.MailboxAttr{`\sent`}, store.UseSent},
		{[]imap.MailboxAttr{imap.MailboxAttrUnmarked, `\Templates`}, store.UseOther},
		{[]imap.MailboxAttr{`\Important`}, store.UseOther},
		{[]imap.MailboxAttr{imap.MailboxAttrHasChildren, imap.MailboxAttrMarked, `\subscribed`}, ""},
		{nil, ""},
	} {
		if got := useOfAttrs(c.attrs); got != c.want {
			t.Errorf("%v: %q, want %q", c.attrs, got, c.want)
		}
	}
}
