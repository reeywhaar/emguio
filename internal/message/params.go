package message

import (
	"fmt"
	"net/url"
	"strings"
)

// Param is a MIME parameter's value, as a server describing a part passes it on: plain, or in
// the RFC 2231 form a long or non-ASCII filename is written in — encoded, name*=utf-8'ru'%D0%9E,
// or split, name*0=…; name*1*=…. Thunderbird writes any non-ASCII filename that way, and a
// server hands the form on undecoded.
func Param(params map[string]string, key string) string {
	if v, ok := params[key]; ok {
		return v
	}
	if v, ok := params[key+"*"]; ok {
		cs, rest := charsetOf(v)
		return inCharset(percent(rest), cs)
	}
	var (
		raw []byte
		cs  string
	)
	for i := 0; ; i++ {
		k := fmt.Sprintf("%s*%d", key, i)
		if v, ok := params[k]; ok {
			raw = append(raw, v...)
			continue
		}
		v, ok := params[k+"*"]
		if !ok {
			break
		}
		if i == 0 {
			cs, v = charsetOf(v)
		}
		raw = append(raw, percent(v)...)
	}
	return inCharset(raw, cs)
}

// charsetOf splits an encoded value, charset'language'text, into its charset and its text.
func charsetOf(v string) (string, string) {
	parts := strings.SplitN(v, "'", 3)
	if len(parts) != 3 {
		return "", v
	}
	return parts[0], parts[2]
}

// percent undoes %XX escapes, keeping what is not one as it stands.
func percent(s string) []byte {
	if out, err := url.PathUnescape(s); err == nil {
		return []byte(out)
	}
	return []byte(s)
}
