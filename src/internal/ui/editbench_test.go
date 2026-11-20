package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Typing into a long mail text (about 40 000 characters) stays fast.
func TestTypingInLongBodyIsFast(t *testing.T) {
	m, _, c := newCompose(t)
	m.applySize(160, 40)
	c.body.SetValue(strings.Repeat("Merhaba dünya, bu uzun bir ileti satırıdır ve 日本語 de var. ", 600))
	c.focus = fBody
	c.applyFocus()
	m.View()
	r := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			m.View()
		}
	})
	ms := float64(r.NsPerOp()) / 1e6
	t.Logf("keypress in a %d-character body: %.3f ms", len([]rune(c.body.Value())), ms)
	if ms > 16 {
		t.Fatalf("typing takes %.2f ms per key", ms)
	}
}
