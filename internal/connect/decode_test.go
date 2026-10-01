package connect

import "testing"

// Tested here rather than through a server: the IMAP server in connecttest shares this process,
// so it borrows the same charsets and decodes headers itself before sending them. A real server
// sends them as the message wrote them, and this is what reads them.
func TestEncodedWordsAreReadInTheCharsetTheyDeclare(t *testing.T) {
	for raw, want := range map[string]string{
		"=?koi8-r?B?8NLJ18XU?=":                    "Привет",
		"=?windows-1251?B?z/Do4uXy?=":              "Привет",
		"=?koi8-r?B?8NLJ18XU?=, отчёт за сентябрь": "Привет, отчёт за сентябрь",
		"=?utf-8?q?caf=C3=A9?= au lait":            "café au lait",
		"Plain words":                              "Plain words",
	} {
		got, err := wordDecoder.DecodeHeader(raw)
		if err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", raw, got, err, want)
		}
	}
}
