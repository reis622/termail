package ui

import (
	"regexp"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"termail/internal/backend"
	"termail/internal/grid"
	"termail/internal/htmltext"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

// Selecting text in the reading pane / an opened mail with the mouse:
// click-drag selects, release keeps the highlight, Ctrl+C / right-click
// copies it. Double-click picks a word, triple-click the line. Shift+click
// extends. Soft line breaks (from wrapping) do not become new lines.

// textView is the mail text as last drawn (set while drawing).
type textView struct {
	ok         bool
	x, y, w, h int
	lines      []wline
	paras      []string
	r          *reader
	key        bodyKey
	att        bool
}

func (tv *textView) contains(x, y int) bool {
	return tv.ok && x >= tv.x && x < tv.x+tv.w && y >= tv.y && y < tv.y+tv.h
}

// tpos: a wrapped line and a cell in it.
type tpos struct{ line, cell int }

func (p tpos) less(q tpos) bool { return p.line < q.line || p.line == q.line && p.cell < q.cell }

type textSel struct {
	key      bodyKey
	att      bool
	w        int
	a, b     tpos // anchor, head (both cells included)
	dragging bool
	moved    bool   // there is something selected (not just a click)
	link     string // pressed on a link: opened on release if nothing was selected
	edge     int    // -1 / +1 while the pointer is above / below the text
	lastX    int
}

type dragTickMsg struct{}

const dragTick = 60 * time.Millisecond

func (s *textSel) valid(tv *textView) bool {
	return s != nil && s.moved && tv.ok && s.key == tv.key && s.att == tv.att && s.w == tv.w
}

func (s *textSel) ordered() (tpos, tpos) {
	if s.b.less(s.a) {
		return s.b, s.a
	}
	return s.a, s.b
}

// cellW is the width a body character takes on screen (as drawBodyLine
// and the grid draw it: link markers one cell, dropped characters none).
func bodyCellW(r rune) int {
	if r == htmltext.MarkRune {
		return 1
	}
	if r >= 0x20 && r < 0x7f {
		return 1
	}
	if grid.Clean(string(r)) == "" {
		return 0
	}
	return runewidth.RuneWidth(r)
}

// byteRange returns the bytes of text covering cells [c0, c1] (inclusive).
func byteRange(text string, c0, c1 int) (int, int) {
	start, end := len(text), len(text)
	x := 0
	found := false
	for i, r := range text {
		w := bodyCellW(r)
		if !found && x+w > c0 {
			start, found = i, true
		}
		if x > c1 {
			end = i
			break
		}
		x += w
	}
	if !found {
		return len(text), len(text)
	}
	return start, end
}

func (m *Model) textPos(x, y int) tpos {
	tv := &m.tv
	line := tv.r.scroll + (y - tv.y)
	line = max(0, min(line, len(tv.lines)-1))
	return tpos{line, max(0, x-tv.x)}
}

// selectedText turns the selection into the original text.
func (m *Model) selectedText() string {
	s, tv := m.rsel, &m.tv
	if !s.valid(tv) || len(tv.lines) == 0 {
		return ""
	}
	a, b := s.ordered()
	var out strings.Builder
	lastPara, lastEnd := -1, 0
	for li := a.line; li <= b.line && li < len(tv.lines); li++ {
		l := tv.lines[li]
		c0, c1 := 0, 1<<30
		if li == a.line {
			c0 = a.cell
		}
		if li == b.line {
			c1 = b.cell
		}
		bs, be := byteRange(l.text, c0, c1)
		bs, be = max(bs, l.pre), max(be, l.pre)
		ss, se := l.start+bs-l.pre, l.start+be-l.pre
		para := tv.paras[l.para]
		if lastPara >= 0 {
			if l.para == lastPara {
				if ss > lastEnd && lastEnd >= 0 && ss <= len(para) {
					out.WriteString(para[lastEnd:ss]) // the space dropped at the soft break
				}
			} else {
				out.WriteString(strings.Repeat("\n", l.para-lastPara))
			}
		}
		if ss < se && se <= len(para) {
			out.WriteString(para[ss:se])
		}
		lastPara, lastEnd = l.para, se
	}
	return markers.ReplaceAllString(out.String(), "")
}

var markers = regexp.MustCompile(`\x{E000}\[\d+\]`)

// paintSel highlights the selected cells of the visible lines.
func (m *Model) paintSel(g *grid.Grid) {
	s, tv := m.rsel, &m.tv
	if !s.valid(tv) {
		return
	}
	a, b := s.ordered()
	for vy := 0; vy < tv.h; vy++ {
		li := tv.r.scroll + vy
		if li < a.line || li > b.line || li >= len(tv.lines) {
			continue
		}
		text := tv.lines[li].text
		width := 0
		for _, r := range text {
			width += bodyCellW(r)
		}
		c0, c1 := 0, width-1
		if li == a.line {
			c0 = a.cell
		}
		if li == b.line {
			c1 = min(c1, b.cell)
		}
		if li != b.line {
			c1 = max(c1, width) // the line break is part of it: one cell more
		}
		if width == 0 {
			c1 = max(c1, 0)
		}
		for c := c0; c <= c1 && c < tv.w; c++ {
			if cell := g.At(tv.x+c, tv.y+vy); cell != nil {
				cell.BG = cTextSel
			}
		}
	}
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '@' || r == '.' || r == '-'
}

// selectWord / selectPara: double / triple click.
func (m *Model) selectWord(p tpos) {
	tv := &m.tv
	text := tv.lines[p.line].text
	type rc struct {
		r    rune
		c, w int
	}
	var rs []rc
	x := 0
	hit := -1
	for _, r := range text {
		w := bodyCellW(r)
		if hit < 0 && x+w > p.cell && w > 0 {
			hit = len(rs)
		}
		rs = append(rs, rc{r, x, w})
		x += w
	}
	if hit < 0 {
		return
	}
	i, j := hit, hit
	if isWordRune(rs[hit].r) {
		for i > 0 && isWordRune(rs[i-1].r) {
			i--
		}
		for j < len(rs)-1 && isWordRune(rs[j+1].r) {
			j++
		}
		for j > i && strings.ContainsRune(".-", rs[j].r) { // "adres." → "adres"
			j--
		}
	}
	m.rsel = &textSel{key: tv.key, att: tv.att, w: tv.w, a: tpos{p.line, rs[i].c}, b: tpos{p.line, rs[j].c + max(1, rs[j].w) - 1}, moved: true}
}

func (m *Model) selectLine(p tpos) {
	tv := &m.tv
	if p.line < 0 || p.line >= len(tv.lines) {
		return
	}
	m.rsel = &textSel{key: tv.key, att: tv.att, w: tv.w, a: tpos{p.line, 0}, b: tpos{p.line, 1 << 20}, moved: true}
}

// copySelection copies the reading-pane selection; false when there is none.
func (m *Model) copySelection() bool {
	t := m.selectedText()
	if t == "" {
		return false
	}
	m.copied(t, "Kopyalandı")
	return true
}

// clearSel drops the selection; reports whether there was one.
func (m *Model) clearSel() bool {
	had := m.rsel != nil && m.rsel.moved
	m.rsel = nil
	return had
}

// ---- mouse ----

// textPress starts a selection (or a word / line one on a double /
// triple click) at (x, y) inside the mail text. Shift+click extends.
func (m *Model) textPress(x, y int, clicks int, link string, shift bool) {
	p := m.textPos(x, y)
	tv := &m.tv
	if shift && m.rsel != nil && m.rsel.valid(tv) {
		m.rsel.b = p
		m.rsel.moved = m.rsel.moved || m.rsel.b != m.rsel.a
		m.rsel.dragging, m.rsel.lastX = true, x
		return
	}
	switch {
	case clicks == 2:
		m.selectWord(p)
	case clicks >= 3:
		m.selectLine(p)
	default:
		m.rsel = &textSel{key: tv.key, att: tv.att, w: tv.w, a: p, b: p, link: link}
	}
	m.rsel.dragging, m.rsel.lastX = true, x
}

// textDrag follows the pointer while the button is held.
func (m *Model) textDrag(x, y int) tea.Cmd {
	s, tv := m.rsel, &m.tv
	if s == nil || !s.dragging || !tv.ok {
		m.noRender = true
		return nil
	}
	s.lastX = x
	edge := 0
	switch {
	case y < tv.y:
		edge = -1
		y = tv.y
	case y >= tv.y+tv.h:
		edge = 1
		y = tv.y + tv.h - 1
	}
	p := m.textPos(max(tv.x, min(x, tv.x+tv.w)), y)
	if edge != 0 {
		m.scrollText(edge)
		p = m.textPos(max(tv.x, min(x, tv.x+tv.w)), y)
	}
	if p != s.b {
		s.b = p
	}
	if s.b != s.a {
		s.moved = true
	}
	start := s.edge == 0 && edge != 0
	s.edge = edge
	if start {
		return tea.Tick(dragTick, func(time.Time) tea.Msg { return dragTickMsg{} })
	}
	return nil
}

func (m *Model) scrollText(d int) {
	tv := &m.tv
	maxScroll := max(0, len(tv.lines)-tv.h)
	tv.r.scroll = max(0, min(tv.r.scroll+d, maxScroll))
}

// onDragTick keeps scrolling while the pointer stays above / below the text.
func (m *Model) onDragTick() tea.Cmd {
	s, tv := m.rsel, &m.tv
	if s == nil || !s.dragging || s.edge == 0 || !tv.ok {
		m.noRender = true
		return nil
	}
	m.scrollText(s.edge)
	y := tv.y
	if s.edge > 0 {
		y = tv.y + tv.h - 1
	}
	s.b = m.textPos(max(tv.x, min(s.lastX, tv.x+tv.w)), y)
	s.moved = s.moved || s.b != s.a
	return tea.Tick(dragTick, func(time.Time) tea.Msg { return dragTickMsg{} })
}

// textRelease ends a drag: the selection stays visible; a plain click
// may open a link. Copy is Ctrl+C or right-click.
func (m *Model) textRelease() tea.Cmd {
	s := m.rsel
	if s == nil || !s.dragging {
		m.noRender = true
		return nil
	}
	s.dragging, s.edge = false, 0
	if s.moved {
		return nil
	}
	link := s.link
	m.rsel = nil
	if link != "" {
		return m.openLink(link)
	}
	return nil
}

// copyBody copies the whole text of a mail (c), or the text attachment shown.
func (m *Model) copyBody(k bodyKey, att bool, ai int) tea.Cmd {
	p := m.body(k)
	if p == nil {
		m.toastInfo(toast.Info, "", "İleti henüz yükleniyor.")
		return nil
	}
	text := p.Text
	if att && ai < len(p.Attachments) {
		text = string(p.Attachments[ai].Data)
	}
	if strings.TrimSpace(text) == "" {
		m.toastInfo(toast.Info, "", "Bu iletide kopyalanacak metin yok.")
		return nil
	}
	m.copied(text, "Metin kopyalandı")
	return nil
}

// copySender copies the sender's address (C).
func (m *Model) copySender(e backend.Envelope, p *mailmsg.Parsed) tea.Cmd {
	addr := e.FromAddr
	if p != nil && len(p.From) > 0 && p.From[0] != nil && p.From[0].Address != "" {
		addr = p.From[0].Address
	}
	if addr == "" {
		m.toastInfo(toast.Info, "", "Gönderen adresi yok.")
		return nil
	}
	m.copied(addr, "Adres kopyalandı: "+addr)
	return nil
}

// withSelKeys puts the selection keys first while mail text is selected.
func (m *Model) withSelKeys(items []keyHint) []keyHint {
	if !m.rsel.valid(&m.tv) {
		return items
	}
	out := []keyHint{{"Ctrl+C", "Seçileni kopyala"}, {"Esc", "Seçimi kaldır"}}
	for _, it := range items {
		if it.key != "Esc" {
			out = append(out, it)
		}
	}
	return out
}
