package mbox

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, m Message, body string) string {
	t.Helper()
	var out bytes.Buffer
	w := NewWriter(&out)
	if err := w.Write(m, strings.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// A message's From lines are escaped one ">" deeper, its CRLFs are LFs, and its flags replace
// any Status it came with.
func TestAMessageIsWrittenAsMboxrd(t *testing.T) {
	at := time.Date(2026, 11, 21, 9, 5, 0, 0, time.UTC)
	got := write(t, Message{From: "ann@example.com", Date: at, Seen: true, Flagged: true},
		"Status: U\r\nSubject: Hi\r\nX-Status: D\r\n continued\r\n\r\nFrom here\r\n>From there\r\nFromage\r\nbye")
	want := "From ann@example.com Sat Nov 21 09:05:00 2026\n" +
		"Status: RO\nX-Status: F\n" +
		"Subject: Hi\n\n" +
		">From here\n>>From there\nFromage\nbye\n\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// A Status in the body is text, not a field.
func TestOnlyTheHeadersStatusIsReplaced(t *testing.T) {
	got := write(t, Message{}, "Subject: x\r\n\r\nStatus: kept\r\n")
	if !strings.Contains(got, "\n\nStatus: kept\n") || !strings.HasPrefix(got, "From MAILER-DAEMON ") {
		t.Errorf("got %q", got)
	}
}

// A line longer than what is read at once stays one line, and a CRLF split across two reads is
// still one line end.
func TestLongLinesAndSplitLineEnds(t *testing.T) {
	long := strings.Repeat("a", 32<<10-1)
	got := write(t, Message{}, "Subject: x\r\n\r\n"+long+"\r\nFrom the end\r\n")
	want := "\n\n" + long + "\n>From the end\n\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("the end is %q", got[len(got)-40:])
	}
	if strings.Contains(got, "\r") {
		t.Error("a CR was left in")
	}
}
