package grid

import (
	"strings"
	"testing"

	"github.com/muesli/termenv"
)

func TestPutTruncateANSI(t *testing.T) {
	g := New(20, 2, "#ffffff", "#000000")
	g.Put(0, 0, "Kasım 2026 faturanız hazır", Style{}, 10)
	if got := rowText(g, 0); !strings.HasPrefix(got, "Kasım 202… ") {
		t.Fatalf("%q", got)
	}
	g.PutANSI(0, 1, "\x1b[38;2;255;0;0mab\x1b[0mc", 20, "#ffffff", "#000000")
	if c := g.At(0, 1); c.FG != "#ff0000" || c.Ch != "a" {
		t.Fatalf("%+v", c)
	}
	if c := g.At(2, 1); c.FG != "#ffffff" || c.Ch != "c" {
		t.Fatalf("%+v", c)
	}
	_ = g.Render(termenv.TrueColor)
}

func rowText(g *Grid, y int) string {
	s := ""
	for x := 0; x < g.W; x++ {
		s += g.At(x, y).Ch
	}
	return s
}
