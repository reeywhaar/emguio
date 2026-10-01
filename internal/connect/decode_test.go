package connect

import "testing"

// Tested here rather than through a server: the IMAP server in connecttest shares this process,
// so it borrows the same charsets and decodes headers itself before sending them. A real server
// sends them as the message wrote them, and this is what reads them.
func TestEncodedWordsAreReadInTheCharsetTheyDeclare(t *testing.T) {
	for raw, want := range map[string]string{
		"=?UTF-8?B?0J/RgNC40LLQtdGC?=":                    "Привет",
		"=?utf-8?q?caf=C3=A9?= au lait":                   "café au lait",
		"=?UTF-8?B?0J/RgNC40LLQtdGC?=, отчёт за сентябрь": "Привет, отчёт за сентябрь",
		"=?gb2312?B?xOO6ww==?=":                           "你好",
		"Plain words":                                     "Plain words",
	} {
		got, err := wordDecoder.DecodeHeader(raw)
		if err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", raw, got, err, want)
		}
	}
}
