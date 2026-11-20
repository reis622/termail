package grid

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"Kasım ayı faturanız İĞÜŞÖÇ ığüşöç": "Kasım ayı faturanız İĞÜŞÖÇ ığüşöç",
		"I\u0307stanbul":                    "İstanbul", // decomposed dotted capital I
		"e\u0301":                           "é",
		"☑️ Ali, finish 🚀":                  " Ali, finish ",
		"a\tb\r\nc\x07d\x1b[31m":            "a b  cd[31m",
		"gizli\u200b\u200d\ufeff\u200eyazı": "gizliyazı",
		"会議 – 東京":                           "会議 – 東京",
		"👩‍👩‍👧‍👦🇹🇷x":                        "x",
		"★ Kaydedilenler ✓ ● ▪ … · ─ │ ╭ ↑ ↓": "★ Kaydedilenler ✓ ● ▪ … · ─ │ ╭ ↑ ↓",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

// Every rendered row is exactly W cells, whatever is drawn into it, also
// when later drawing overwrites half of a wide character.
func TestRenderRowsExactWidth(t *testing.T) {
	g := New(20, 3, "#ffffff", "#000000")
	g.Put(0, 0, "会議のお知らせ東京オフィス", Style{}, 0)
	g.Put(3, 0, "x", Style{}, 0) // cuts a wide char in half
	g.Put(0, 1, "☑️\t\r\nİstanbul 🎉 çok uzun bir konu satırı", Style{}, 0)
	g.Put(19, 2, "会", Style{}, 0) // does not fit in the last cell
	for i, l := range strings.Split(g.Render(termenv.TrueColor), "\n") {
		if w := ansi.StringWidth(l); w != 20 {
			t.Fatalf("row %d is %d cells: %q", i, w, ansi.Strip(l))
		}
	}
}
