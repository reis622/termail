package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/edit"
	"termail/internal/grid"
)

// In-mail search (Ctrl+F or / in the reader pane or a reader tab).
type bodyFind struct {
	in      edit.Editor
	cur     int // 0-based index into matches, -1 if none
	matches []findHit
}

type findHit struct {
	line, start, end int // wrapped-line index, cell range [start, end)
}

func (m *Model) openBodyFind() tea.Cmd {
	r := m.activeReader()
	if r == nil {
		return nil
	}
	if m.bodyFind == nil {
		in := edit.New(edit.Single)
		in.Placeholder = "İletide ara"
		m.bodyFind = &bodyFind{in: in, cur: -1}
	}
	m.bodyFind.in.Focus()
	m.refreshBodyFind()
	return nil
}

func (m *Model) closeBodyFind() {
	m.bodyFind = nil
}

func (m *Model) bodyFindKey(k tea.KeyMsg) tea.Cmd {
	f := m.bodyFind
	if f == nil {
		return nil
	}
	key := k.String()
	switch key {
	case "esc":
		m.closeBodyFind()
		return nil
	case "enter":
		m.stepBodyFind(1)
		return nil
	case "shift+enter":
		m.stepBodyFind(-1)
		return nil
	case "up":
		m.stepBodyFind(-1)
		return nil
	case "down":
		m.stepBodyFind(1)
		return nil
	}
	_, cmd := m.editKey(&f.in, k)
	m.refreshBodyFind()
	return cmd
}

func (m *Model) refreshBodyFind() {
	f := m.bodyFind
	r := m.activeReader()
	if f == nil || r == nil {
		return
	}
	q := strings.TrimSpace(f.in.Value())
	f.matches = nil
	f.cur = -1
	if q == "" || !m.tv.ok || len(m.tv.lines) == 0 {
		return
	}
	ql := foldFind(q)
	for i, l := range m.tv.lines {
		low := foldFind(l.text)
		from := 0
		for {
			j := strings.Index(low[from:], ql)
			if j < 0 {
				break
			}
			j += from
			// map folded index back approximately via original runes
			c0 := findCellAt(l.text, byteIndexFold(l.text, j))
			c1 := findCellAt(l.text, byteIndexFold(l.text, j+len(ql)))
			if c1 <= c0 {
				c1 = c0 + grid.Width(q)
			}
			f.matches = append(f.matches, findHit{line: i, start: c0, end: c1})
			from = j + max(1, len(ql))
		}
	}
	if len(f.matches) > 0 {
		f.cur = 0
		m.scrollToFind()
	}
}

func findCellAt(text string, byteIdx int) int {
	if byteIdx <= 0 {
		return 0
	}
	if byteIdx >= len(text) {
		x := 0
		for _, r := range text {
			x += bodyCellW(r)
		}
		return x
	}
	x, i := 0, 0
	for _, r := range text {
		if i >= byteIdx {
			return x
		}
		i += len(string(r))
		x += bodyCellW(r)
	}
	return x
}

func (m *Model) stepBodyFind(d int) {
	f := m.bodyFind
	if f == nil || len(f.matches) == 0 {
		return
	}
	f.cur = (f.cur + d) % len(f.matches)
	if f.cur < 0 {
		f.cur += len(f.matches)
	}
	m.scrollToFind()
}

func (m *Model) scrollToFind() {
	f, r := m.bodyFind, m.activeReader()
	if f == nil || r == nil || f.cur < 0 || f.cur >= len(f.matches) || !m.tv.ok {
		return
	}
	li := f.matches[f.cur].line
	if li < r.scroll {
		r.scroll = li
	}
	if li >= r.scroll+max(1, m.tv.h) {
		r.scroll = max(0, li-max(1, m.tv.h)+1)
	}
}

func (m *Model) paintBodyFind(g *grid.Grid) {
	f := m.bodyFind
	tv := &m.tv
	if f == nil || !tv.ok || len(f.matches) == 0 {
		return
	}
	for i, h := range f.matches {
		if h.line < tv.r.scroll || h.line >= tv.r.scroll+tv.h {
			continue
		}
		vy := tv.y + (h.line - tv.r.scroll)
		bg := cChip
		if i == f.cur {
			bg = cTextSel
		}
		for c := h.start; c < h.end && c < tv.w; c++ {
			if cell := g.At(tv.x+c, vy); cell != nil {
				cell.BG = bg
			}
		}
	}
}

func (m *Model) drawBodyFind(g *grid.Grid, x, y, w int) {
	f := m.bodyFind
	if f == nil || w < 8 {
		return
	}
	g.Fill(x, y, w, 1, cRaised)
	lab := "Ara: "
	x0 := g.Put(x+1, y, lab, stbg(cAccent, cRaised), 0)
	nm := ""
	if q := strings.TrimSpace(f.in.Value()); q != "" {
		if len(f.matches) == 0 {
			nm = "0/0"
		} else {
			nm = fmt.Sprintf("%d/%d", f.cur+1, len(f.matches))
		}
	}
	fw := max(4, w-grid.Width(lab)-grid.Width(nm)-4)
	f.in.Draw(g, x0, y, fw, 1, editStyle(cRaised))
	m.hits.add(hit{x: x0, y: y, w: fw, h: 1, kind: hitMailFind, ox: x0, oy: y})
	if nm != "" {
		g.RPut(x+w-1, y, nm, stbg(cAccent, cRaised))
	}
}


func byteIndexFold(s string, foldedByte int) int {
	if foldedByte <= 0 {
		return 0
	}
	n, i := 0, 0
	for _, r := range s {
		fr := foldFind(string(r))
		if n >= foldedByte {
			return i
		}
		n += len(fr)
		i += len(string(r))
	}
	return len(s)
}

// runeFold is kept for possible case folding of Turkish I; search uses ToLower
// which is enough for ascii and most Turkish letters except İ/I, handled here
// when comparing if needed.
func foldFind(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == 'İ' || r == 'I' {
			b.WriteRune('i')
			continue
		}
		if r == 'ı' {
			b.WriteRune('i')
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
