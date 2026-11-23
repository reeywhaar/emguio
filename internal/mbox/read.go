package mbox

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/mail"
	"strings"
	"time"
)

// Span is one message of an mbox file: its text from Start to End, after its From_ line, and
// the date that line gives.
type Span struct {
	Start, End int64
	Date       time.Time
}

// Scan finds the messages of an mbox, reading it once and holding none of it. A From_ line is
// one that begins "From " at the start of the file or after an empty line: an mbox that escaped
// only those is read too.
func Scan(r io.Reader) ([]Span, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var (
		spans []Span
		at    int64
		start = true // the next bytes begin a line
		empty = true // the line before was empty, or there was none
	)
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 {
			if start && empty && bytes.HasPrefix(chunk, []byte("From ")) {
				if n := len(spans); n > 0 {
					spans[n-1].End = at
				}
				line := chunk
				// A From_ line longer than what is read at once is cut; its date is not needed.
				for errors.Is(err, bufio.ErrBufferFull) {
					at += int64(len(chunk))
					chunk, err = br.ReadSlice('\n')
				}
				at += int64(len(chunk))
				spans = append(spans, Span{Start: at, Date: fromDate(line)})
				start, empty = true, false
				if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
					break
				}
				continue
			}
			complete := chunk[len(chunk)-1] == '\n'
			if start {
				empty = len(bytes.TrimRight(chunk, "\r\n")) == 0 && complete
			} else if complete {
				empty = false
			}
			at += int64(len(chunk))
			start = complete
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if n := len(spans); n > 0 {
		spans[n-1].End = at
	}
	return spans, nil
}

// fromDate is the date at the end of a From_ line, as asctime writes it; zero when it has none.
func fromDate(line []byte) time.Time {
	fields := strings.Fields(string(line))
	if len(fields) < 7 {
		return time.Time{}
	}
	at, err := time.Parse(time.ANSIC, strings.Join(fields[len(fields)-5:], " "))
	if err != nil {
		return time.Time{}
	}
	return at
}

// Head is what Convert learned of a message on the way through.
type Head struct {
	// Size is how many bytes it wrote.
	Size      int64
	MessageID string
	// Date is the Date field's, zero when it has none that reads.
	Date time.Time
	// Status is whether it had a Status field, which the flags below are read from.
	Status                         bool
	Seen, Answered, Flagged, Draft bool
}

// Convert writes a message as a mail server takes it: every line ended by CRLF, and Status and
// X-Status left out, their flags in Head. For a message of an mbox, mbox is set: a line escaped
// as ">From " loses one ">", and the empty line that ends it goes. Nothing is held whole.
func Convert(r io.Reader, w io.Writer, mbox bool) (Head, error) {
	var h Head
	bw := bufio.NewWriterSize(&counter{w: w, n: &h.Size}, 32<<10)
	br := bufio.NewReaderSize(r, 32<<10)
	var (
		start    = true
		header   = true
		dropping = false
		skipping = false
		// held is an empty line not yet written: the last one of an mbox message is its end.
		held = false
		// cr is a CR that ended a chunk, held back in case the LF of a CRLF comes next.
		cr = false
		// field is the header field under way, for the few read: Message-ID, Date, the status.
		field strings.Builder
		named string
	)
	finish := func() {
		value := strings.TrimSpace(field.String())
		switch named {
		case "message-id":
			h.MessageID = value
		case "date":
			if at, err := mail.ParseDate(value); err == nil {
				h.Date = at
			}
		case "status":
			h.Status = true
			h.Seen = strings.Contains(value, "R")
		case "x-status":
			h.Answered = strings.Contains(value, "A")
			h.Flagged = strings.Contains(value, "F")
			h.Draft = strings.Contains(value, "T")
		}
		named = ""
		field.Reset()
	}
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 {
			complete := chunk[len(chunk)-1] == '\n'
			text := chunk
			if complete {
				text = bytes.TrimSuffix(chunk[:len(chunk)-1], []byte("\r"))
			}
			if start {
				dropping = false
				if header {
					switch {
					case len(text) == 0 && complete:
						finish()
						header, skipping = false, false
					case text[0] == ' ' || text[0] == '\t':
						dropping = skipping
						if named != "" {
							field.Write(text)
						}
					default:
						finish()
						name, value, _ := bytes.Cut(text, []byte(":"))
						named = strings.ToLower(strings.TrimSpace(string(name)))
						switch named {
						case "message-id", "date", "status", "x-status":
							field.Write(value)
						default:
							named = ""
						}
						skipping = named == "status" || named == "x-status"
						dropping = skipping
					}
				} else if mbox && len(text) > 1 && text[0] == '>' && bytes.HasPrefix(bytes.TrimLeft(text, ">"), []byte("From ")) {
					text = text[1:]
				}
			} else if header && named != "" {
				field.Write(text)
			}
			if dropping {
				cr = false
			} else {
				if cr && chunk[0] != '\n' {
					bw.WriteByte('\r')
				}
				cr = false
				if start && complete && len(text) == 0 && mbox && !header {
					if held {
						bw.WriteString("\r\n")
					}
					held = true
				} else {
					if held {
						bw.WriteString("\r\n")
						held = false
					}
					if !complete && len(text) > 0 && text[len(text)-1] == '\r' {
						cr, text = true, text[:len(text)-1]
					}
					bw.Write(text)
					if complete {
						bw.WriteString("\r\n")
					}
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
			return h, err
		}
	}
	if header {
		finish()
	}
	if cr {
		bw.WriteByte('\r')
	}
	if !start {
		bw.WriteString("\r\n")
	}
	return h, bw.Flush()
}

type counter struct {
	w io.Writer
	n *int64
}

func (c *counter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	*c.n += int64(n)
	return n, err
}
