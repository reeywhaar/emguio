package message

import "testing"

func TestTheWaysToUnsubscribeAreReadFromTheHeader(t *testing.T) {
	for _, c := range []struct {
		name, header string
		want         Unsubscribe
	}{
		{"both, one click",
			"List-Unsubscribe: <mailto:unsub@news.example.com?subject=unsubscribe>, <https://news.example.com/u/abc>\r\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n",
			Unsubscribe{Link: "https://news.example.com/u/abc", OneClick: true}},
		{"a link to open",
			"List-Unsubscribe: <https://news.example.com/u/abc>\r\n",
			Unsubscribe{Link: "https://news.example.com/u/abc"}},
		{"one click needs https",
			"List-Unsubscribe: <http://news.example.com/u/abc>\r\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n",
			Unsubscribe{Link: "http://news.example.com/u/abc"}},
		{"an email address alone is not a way",
			"List-Unsubscribe: <mailto:leave@lists.example.org>\r\n",
			Unsubscribe{}},
		{"folded inside the brackets",
			"List-Unsubscribe: <https://news.example.com/u/\r\n abc?x=1>\r\n",
			Unsubscribe{Link: "https://news.example.com/u/abc?x=1"}},
		{"names in any case, a comment between",
			"list-unsubscribe: (go away) <HTTPS://news.example.com/u>\r\nLIST-UNSUBSCRIBE-POST: list-unsubscribe=one-click\r\n",
			Unsubscribe{Link: "HTTPS://news.example.com/u", OneClick: true}},
		{"nothing usable",
			"List-Unsubscribe: <ftp://news.example.com/u>, not an address\r\n",
			Unsubscribe{}},
		{"none at all", "", Unsubscribe{}},
	} {
		if got := UnsubscribeOf([]byte(c.header)); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}
