package compose

import (
	"bytes"
	"errors"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/jhillyerd/enmime/v2"
)

// What is built reads back as what was written, with Bcc among the recipients and in no header.
func TestAMessageReadsBackAsWritten(t *testing.T) {
	built, err := Build(Mail{
		From:        mail.Address{Name: "Миша", Address: "misha@example.com"},
		To:          []mail.Address{{Name: "Robin", Address: "robin@example.com"}},
		Cc:          []mail.Address{{Address: "kim@example.com"}},
		Bcc:         []mail.Address{{Address: "secret@example.com"}},
		Subject:     "Счёт за октябрь",
		Text:        "Hello,\nthe invoice is attached.\n",
		Attachments: []Attachment{{Name: "invoice.pdf", Type: "application/pdf", Data: []byte("%PDF-1.7")}},
		InReplyTo:   "<a@example.com>",
		References:  []string{"<root@example.com>", "<a@example.com>"},
		Date:        time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(built.Recipients, ","); got != "robin@example.com,kim@example.com,secret@example.com" {
		t.Errorf("recipients = %s", got)
	}
	if !strings.HasPrefix(built.ID, "<") || !strings.HasSuffix(built.ID, "@example.com>") {
		t.Errorf("id = %s", built.ID)
	}
	if bytes.Contains(built.Raw, []byte("secret@example.com")) {
		t.Error("the Bcc is in the message")
	}

	env, err := enmime.ReadEnvelope(bytes.NewReader(built.Raw))
	if err != nil {
		t.Fatal(err)
	}
	for header, want := range map[string]string{
		"From":        "Миша <misha@example.com>",
		"To":          "\"Robin\" <robin@example.com>",
		"Cc":          "<kim@example.com>",
		"Subject":     "Счёт за октябрь",
		"Message-Id":  built.ID,
		"In-Reply-To": "<a@example.com>",
		"References":  "<root@example.com> <a@example.com>",
	} {
		if got := env.GetHeader(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if env.Text != "Hello,\r\nthe invoice is attached.\r\n" && env.Text != "Hello,\nthe invoice is attached.\n" {
		t.Errorf("text = %q", env.Text)
	}
	if len(env.Attachments) != 1 || env.Attachments[0].FileName != "invoice.pdf" || string(env.Attachments[0].Content) != "%PDF-1.7" {
		t.Errorf("attachments = %+v", env.Attachments)
	}
}

func TestAFieldIsReadAsAddresses(t *testing.T) {
	list, err := Addresses("To", ` Robin <robin@example.com>, "Kim, K." <kim@example.com>,bob@example.com `)
	if err != nil || len(list) != 3 || list[1].Name != "Kim, K." || list[2].Address != "bob@example.com" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list, err := Addresses("Cc", "  "); err != nil || list != nil {
		t.Errorf("empty = %+v, %v", list, err)
	}
	_, err = Addresses("To", "robin@example.com, bob@")
	var bad *Unaddressable
	if !errors.As(err, &bad) || err.Error() != "“bob@” in To is not an address." {
		t.Errorf("err = %v", err)
	}
}
