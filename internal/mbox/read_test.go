package mbox

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// What Write writes, Scan and Convert read back as the server held it, flags and all.
func TestAnMboxReadsBackAsItWasWritten(t *testing.T) {
	at := time.Date(2026, 11, 21, 9, 5, 0, 0, time.UTC)
	texts := []string{
		"Message-ID: <a@example.com>\r\nDate: Sat, 21 Nov 2026 09:00:00 +0000\r\nSubject: one\r\n\r\nFrom me\r\n>From you\r\n\r\nend\r\n",
		"Subject: two\r\n\r\n",
		"Subject: three\r\n\r\nno end",
	}
	flags := []Message{{From: "ann@example.com", Date: at, Seen: true, Answered: true}, {Date: at.Add(time.Hour), Flagged: true}, {}}
	var file bytes.Buffer
	w := NewWriter(&file)
	for i, text := range texts {
		if err := w.Write(flags[i], strings.NewReader(text)); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()

	spans, err := Scan(bytes.NewReader(file.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 3 || !spans[0].Date.Equal(at) || !spans[1].Date.Equal(at.Add(time.Hour)) {
		t.Fatalf("spans = %+v", spans)
	}
	wants := []string{texts[0], texts[1], "Subject: three\r\n\r\nno end\r\n"}
	for i, sp := range spans {
		var out bytes.Buffer
		h, err := Convert(io.NewSectionReader(bytes.NewReader(file.Bytes()), sp.Start, sp.End-sp.Start), &out, true)
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != wants[i] || h.Size != int64(out.Len()) {
			t.Errorf("message %d =\n%q\nwant\n%q", i, out.String(), wants[i])
		}
		if !h.Status || h.Seen != flags[i].Seen || h.Answered != flags[i].Answered || h.Flagged != flags[i].Flagged {
			t.Errorf("message %d head = %+v", i, h)
		}
	}
	h, _ := Convert(io.NewSectionReader(bytes.NewReader(file.Bytes()), spans[0].Start, spans[0].End-spans[0].Start), io.Discard, true)
	if h.MessageID != "<a@example.com>" || !h.Date.Equal(at.Add(-5*time.Minute)) {
		t.Errorf("head = %+v", h)
	}
}

// An eml written on Unix ends its lines with LF alone; a server takes CRLF. Nothing is unescaped.
func TestAnEmlGetsCRLFs(t *testing.T) {
	var out bytes.Buffer
	h, err := Convert(strings.NewReader("Subject: x\nMessage-ID:\n <b@example.com>\n\n>From here\n\n"), &out, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "Subject: x\r\nMessage-ID:\r\n <b@example.com>\r\n\r\n>From here\r\n\r\n" {
		t.Errorf("got %q", out.String())
	}
	if h.MessageID != "<b@example.com>" || h.Status {
		t.Errorf("head = %+v", h)
	}
}

// A CRLF split across two reads is one line end.
func TestASplitCRLFIsOneLineEnd(t *testing.T) {
	long := strings.Repeat("a", 32<<10-1)
	var out bytes.Buffer
	if _, err := Convert(strings.NewReader("Subject: x\r\n\r\n"+long+"\r\nb\r\n"), &out, false); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Subject: x\r\n\r\n"+long+"\r\nb\r\n" {
		t.Errorf("the end is %q", out.String()[out.Len()-10:])
	}
}

// A From line in the middle of text, with no empty line before it, is text.
func TestOnlyAFromLineAfterAnEmptyOneStartsAMessage(t *testing.T) {
	spans, err := Scan(strings.NewReader("From a Sat Nov 21 09:05:00 2026\nSubject: x\n\nhi\nFrom b in the text\n\nFrom c Sat Nov 21 10:05:00 2026\nSubject: y\n\n"))
	if err != nil || len(spans) != 2 {
		t.Fatalf("spans = %+v, %v", spans, err)
	}
}
