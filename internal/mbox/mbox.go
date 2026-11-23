// Package mbox writes mail as mboxrd: messages one after another, each after a From_ line, its
// lines that begin with ">*From " given one more ">", so the escaping can always be undone. Read,
// answered, flagged and draft go in Status and X-Status, as mutt and Thunderbird read them. See
// docs/reading.md.
package mbox

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"time"
)

// Message is what a message's lines around its text say.
type Message struct {
	// From is the envelope sender, for the From_ line; MAILER-DAEMON when empty.
	From string
	// Date is when it arrived.
	Date time.Time

	Seen, Answered, Flagged, Draft bool
}

// Writer writes messages to an mbox as they are read, holding none of one whole.
type Writer struct {
	w *bufio.Writer
}

func NewWriter(w io.Writer) *Writer { return &Writer{w: bufio.NewWriterSize(w, 64<<10)} }

// Flush writes out what is buffered.
func (w *Writer) Flush() error { return w.w.Flush() }

// Write writes one message: its From_ line, its flags as Status and X-Status in place of any it
// had, then its text from body as the server holds it, CRLF as LF and From lines escaped.
func (w *Writer) Write(m Message, body io.Reader) error {
	from := m.From
	if from == "" || bytes.ContainsAny([]byte(from), " \t\r\n") {
		from = "MAILER-DAEMON"
	}
	date := m.Date
	if date.IsZero() {
		date = time.Unix(0, 0)
	}
	w.w.WriteString("From " + from + " " + date.UTC().Format(time.ANSIC) + "\n")
	if m.Seen {
		w.w.WriteString("Status: RO\n")
	} else {
		w.w.WriteString("Status: O\n")
	}
	var x []byte
	for _, f := range []struct {
		on   bool
		mark byte
	}{{m.Answered, 'A'}, {m.Flagged, 'F'}, {m.Draft, 'T'}} {
		if f.on {
			x = append(x, f.mark)
		}
	}
	if len(x) > 0 {
		w.w.WriteString("X-Status: " + string(x) + "\n")
	}

	br := bufio.NewReaderSize(body, 32<<10)
	var (
		start    = true  // the next bytes begin a line
		header   = true  // still in the message's header
		skipping = false // in a Status or X-Status field, continuation lines included
		dropping = false // the line under way is left out
		cr       = false // a CR held back, in case the LF of a CRLF follows
		last     = byte('\n')
	)
	put := func(b []byte) {
		if len(b) > 0 {
			w.w.Write(b)
			last = b[len(b)-1]
		}
	}
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 {
			if start {
				dropping = false
				if header {
					switch {
					case bytes.Equal(chunk, []byte("\n")) || bytes.Equal(chunk, []byte("\r\n")):
						header, skipping = false, false
					case chunk[0] == ' ' || chunk[0] == '\t':
						dropping = skipping
					default:
						skipping = field(chunk, "status:") || field(chunk, "x-status:")
						dropping = skipping
					}
				}
				if !dropping && escaped(chunk) {
					put([]byte{'>'})
				}
			}
			complete := chunk[len(chunk)-1] == '\n'
			if dropping {
				cr = false
			} else {
				if cr && chunk[0] != '\n' {
					put([]byte{'\r'})
				}
				cr = false
				text := chunk
				if complete {
					text = bytes.TrimSuffix(chunk[:len(chunk)-1], []byte("\r"))
				} else if text[len(text)-1] == '\r' {
					cr, text = true, text[:len(text)-1]
				}
				put(text)
				if complete {
					put([]byte{'\n'})
				}
			}
			start = complete
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if cr {
		put([]byte{'\r'})
	}
	if last != '\n' {
		w.w.WriteByte('\n')
	}
	// The blank line that ends a message.
	_, err := w.w.WriteString("\n")
	return err
}

// field is whether a header line is the named field, its name in lower case with the colon.
func field(line []byte, name string) bool {
	return len(line) >= len(name) && bytes.EqualFold(line[:len(name)], []byte(name))
}

// escaped is whether a line reads as a From_ line once its ">"s are taken off: it gets one more.
func escaped(line []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(line, ">"), []byte("From "))
}
