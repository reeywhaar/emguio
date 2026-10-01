// Package ids mints and validates every identifier in emguio.
//
// Three kinds, and one rule decides between them: a software-only identifier is a ULID, an
// identifier a person or a model types is short, and an identifier the caller has to compute
// without asking is derived. See docs/conventions.md.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
	Message     = "m_"
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
