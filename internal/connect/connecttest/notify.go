package connecttest

import (
	"bufio"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Notifier is an IMAP server here with NOTIFY (RFC 5465), which the one it fronts lacks: it takes
// NOTIFY itself, and a test says for it what a server with NOTIFY would say unasked.
type Notifier struct {
	mu      sync.Mutex
	asked   []string
	clients []*notified
	// Refuse, when set, is whether to answer a NOTIFY with BAD, given what it asked.
	Refuse func(asked string) bool
}

// notified is one client's connection, written to one whole response at a time, so what is
// told in between never lands inside one.
type notified struct {
	mu sync.Mutex
	w  io.Writer
	on bool
}

func (c *notified) write(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.w.Write(b)
	return err
}

// IMAPNotifying is IMAPSaying with "implicit" TLS and NOTIFY. said sees what passes to the server
// behind, which is everything but NOTIFY and what is told.
func IMAPNotifying(t testing.TB, cert *Cert, username, password string, said io.Writer) (int, *Notifier) {
	t.Helper()
	behind := IMAPSaying(t, cert, "", username, password, Modern, said)
	n := &Notifier{}
	ln := listen(t, cert, "implicit")
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go n.serve(t, conn, behind)
		}
	}()
	return port(ln), n
}

// Tell says line, an untagged response, to every client NOTIFY is set for.
func (n *Notifier) Tell(line string) {
	n.mu.Lock()
	clients := n.clients
	n.mu.Unlock()
	for _, c := range clients {
		c.mu.Lock()
		on := c.on
		c.mu.Unlock()
		if on {
			c.write([]byte(line + "\r\n"))
		}
	}
}

// Asked is what each NOTIFY asked for, in order.
func (n *Notifier) Asked() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.asked...)
}

var (
	// literal ends a line followed by that many bytes as they are.
	literal       = regexp.MustCompile(`\{(\d+)\+?\}\r?\n$`)
	notifyCommand = regexp.MustCompile(`(?i)^(\S+) NOTIFY (.*?)\r?\n$`)
	capability    = regexp.MustCompile(`^(\* CAPABILITY|\* OK \[CAPABILITY|\S+ OK \[CAPABILITY)`)
)

func (n *Notifier) serve(t testing.TB, client net.Conn, behind int) {
	defer client.Close()
	server, err := net.Dial("tcp", net.JoinHostPort(Host, strconv.Itoa(behind)))
	if err != nil {
		t.Log(err)
		return
	}
	defer server.Close()
	c := &notified{w: client}
	n.mu.Lock()
	n.clients = append(n.clients, c)
	n.mu.Unlock()

	go func() {
		defer client.Close()
		r := bufio.NewReader(server)
		for {
			response, err := whole(r)
			if err != nil {
				return
			}
			// The server behind says only what it has itself.
			response = capability.ReplaceAll(response, []byte("${1} NOTIFY"))
			if c.write(response) != nil {
				return
			}
		}
	}()

	// A client sends a literal's bytes only once told to go on, so its lines go through as they
	// come.
	r := bufio.NewReader(client)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if m := notifyCommand.FindStringSubmatch(line); m != nil {
			n.mu.Lock()
			n.asked = append(n.asked, m[2])
			refuse := n.Refuse != nil && n.Refuse(m[2])
			n.mu.Unlock()
			if refuse {
				c.write([]byte(m[1] + " BAD NOTIFY refused\r\n"))
				continue
			}
			c.mu.Lock()
			c.on = !strings.EqualFold(m[2], "NONE")
			c.mu.Unlock()
			c.write([]byte(m[1] + " OK NOTIFY done\r\n"))
			continue
		}
		if _, err := io.WriteString(server, line); err != nil {
			return
		}
		if m := literal.FindStringSubmatch(line); m != nil {
			size, _ := strconv.ParseInt(m[1], 10, 64)
			if _, err := io.CopyN(server, r, size); err != nil {
				return
			}
		}
	}
}

// whole reads one response, its literals and the lines after them included.
func whole(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		out = append(out, line...)
		m := literal.FindStringSubmatch(line)
		if m == nil {
			return out, nil
		}
		size, _ := strconv.Atoi(m[1])
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}
		out = append(out, data...)
	}
}
