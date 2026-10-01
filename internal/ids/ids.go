// Package ids mints and validates every identifier in emguio.
//
// Three kinds, and one rule decides between them: a software-only identifier is a ULID, an
// identifier a person or a model types is short, and an identifier the caller has to compute
// without asking is derived. A message is the exception, named by its server — see
// docs/conventions.md.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Alphabet is Crockford's base32, lowercased.
//
// It omits i, l, o and u, so an id cannot be misread between similar glyphs or accidentally
// spell a word. Lowercase because an id is read inside a URL.
const Alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

// Prefixes.
const (
	User        = "u_"
	Invite      = "i_"
	Session     = "s_"
	EmailConfig = "ec_"
	Mailbox     = "mb_"
	Job         = "j_"
	Draft       = "d_"
	DraftPart   = "dp_"
)

const (
	// ulidLen is 26 characters over 16 bytes: a 6-byte big-endian millisecond timestamp then
	// 10 random, so ids sort chronologically.
	ulidLen = 26
	// derivedLen is how much of a hash names something. 12 hex characters is 48 bits.
	derivedLen = 12
)

// New mints a prefixed ULID: a 6-byte big-endian millisecond timestamp then 10 random bytes,
// so ORDER BY id is a time order.
func New(prefix string, unixMilli int64) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(unixMilli)<<16)
	rand.Read(b[6:])
	return prefix + encode32(b[:], ulidLen)
}

// Derive names something by hashing it, so whoever holds the thing can work the id out without
// asking the server first.
func Derive(prefix string, of []byte) string {
	sum := sha256.Sum256(of)
	return prefix + hex.EncodeToString(sum[:])[:derivedLen]
}

// Valid reports whether id is a well-formed identifier of the given prefix.
//
// Called before a value reaches a query, so a typo is a refusal naming the problem rather than
// an empty result that looks like a missing row.
func Valid(prefix, id string) bool {
	if !strings.HasPrefix(id, prefix) {
		return false
	}
	body := id[len(prefix):]
	switch len(body) {
	case ulidLen:
		for i := 0; i < len(body); i++ {
			if !strings.ContainsRune(Alphabet, rune(body[i])) {
				return false
			}
		}
		return true
	case derivedLen:
		_, err := hex.DecodeString(body)
		return err == nil
	}
	return false
}

// Message names a message within its mailbox by what the server calls it: the mailbox's
// UIDVALIDITY and the message's UID. emguio keeps no row for most messages, so there is nothing
// of its own to name them by. A dash between them rather than a dot, because a path ending in a
// dot and digits reads as a file to whatever serves it.
func Message(uidValidity, uid uint32) string {
	return fmt.Sprintf("%d-%d", uidValidity, uid)
}

// ParseMessage reads a message id back. Both numbers are non-zero 32-bit integers, which is all
// IMAP allows them to be.
func ParseMessage(id string) (uidValidity, uid uint32, ok bool) {
	a, b, found := strings.Cut(id, "-")
	if !found {
		return 0, 0, false
	}
	v, err1 := strconv.ParseUint(a, 10, 32)
	u, err2 := strconv.ParseUint(b, 10, 32)
	if err1 != nil || err2 != nil || v == 0 || u == 0 || a[0] == '0' || b[0] == '0' {
		return 0, 0, false
	}
	return uint32(v), uint32(u), true
}

// encode32 renders b as n characters of the alphabet, most significant first.
func encode32(b []byte, n int) string {
	out := make([]byte, n)
	var acc, bits uint32
	i := n
	for j := len(b) - 1; j >= 0; j-- {
		acc |= uint32(b[j]) << bits
		bits += 8
		for bits >= 5 && i > 0 {
			i--
			out[i] = Alphabet[acc&31]
			acc >>= 5
			bits -= 5
		}
	}
	for i > 0 {
		i--
		out[i] = Alphabet[acc&31]
		acc >>= 5
	}
	return string(out)
}
