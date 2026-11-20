// Package edit is Termail's own text field: one line (addresses, subject,
// search, sign-in) or many lines with word wrap (the mail text). Unlike the
// bubbles fields it has a selection (Shift+arrows, Ctrl+A, mouse), copy /
// cut / paste hooks, undo, and it draws itself straight into the cell grid,
// so wide (CJK) and Turkish letters always line up.
//
// The editor never touches the clipboard itself: Key reports what to copy
// (Result.Copy) or that a paste was asked for (Result.Paste); the caller
// reads / writes the clipboard and calls Insert.
package edit

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/norm"

	"termail/internal/grid"
)

// Limits for pasted text.
const (
	MaxBody = 1 << 20 // characters in the mail text
	MaxLine = 8000    // characters in a one-line field
	maxUndo = 100
)

// Pos is a place in the text: line and character (rune) index.
type Pos struct{ Line, Col int }

func (p Pos) less(q Pos) bool { return p.Line < q.Line || p.Line == q.Line && p.Col < q.Col }

// Kind of field.
type Kind int

const (
	Single  Kind = iota // one line
	Address             // one line; pasted lines / ";" become ", "
	Multi               // many lines, word wrapped
)

type snap struct {
	text        string
	cur, anchor Pos
	sel         bool
}

type vrow struct{ line, start, end int } // a visual row: runes [start,end) of a line

// Editor is a text field. The zero value is not ready; use New.
type Editor struct {
	Kind        Kind
	Placeholder string
	Mask        rune // non-zero: a password field (drawn masked, never copied)
	CharLimit   int  // in characters; 0: no limit

	lines       [][]rune
	cur, anchor Pos
	sel         bool
	focused     bool
	w, h        int
	top, off    int  // first visual row shown (Multi) / first cell shown (one line)
	goal        int  // wanted cell column for ↑↓ (-1: none)
	follow      bool // the cursor moved: scroll it into view on the next Draw

	undo, redo []snap
	typing     bool // the last change was plain typing (coalesced into one undo step)

	val   string
	valOK bool
	rows  []vrow
	rowsW int // width rows were made for (-1: stale)
}

// New returns an empty field.
func New(k Kind) Editor {
	return Editor{Kind: k, lines: [][]rune{{}}, goal: -1, rowsW: -1, w: 20, h: 1}
}

func (e *Editor) init() {
	if len(e.lines) == 0 {
		e.lines = [][]rune{{}}
		e.goal, e.rowsW = -1, -1
	}
}

func (e *Editor) multi() bool { return e.Kind == Multi }

// ---- value ----

// Value is the whole text (lines joined with "\n").
func (e *Editor) Value() string {
	e.init()
	if !e.valOK {
		if len(e.lines) == 1 {
			e.val = string(e.lines[0])
		} else {
			var b strings.Builder
			for i, l := range e.lines {
				if i > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(string(l))
			}
			e.val = b.String()
		}
		e.valOK = true
	}
	return e.val
}

// SetValue replaces the text (cursor to the end, selection and undo history
// cleared). Text is cleaned like a paste, but not shortened.
func (e *Editor) SetValue(s string) {
	e.init()
	s = Normalize(s, e.Kind)
	e.setText(s)
	e.CursorEnd()
	e.undo, e.redo, e.typing = nil, nil, false
}

func (e *Editor) setText(s string) {
	parts := strings.Split(s, "\n")
	e.lines = make([][]rune, len(parts))
	for i, p := range parts {
		e.lines[i] = []rune(p)
	}
	e.changed()
}

func (e *Editor) changed() {
	e.valOK = false
	e.rowsW = -1
}

// ---- focus / size ----

// Focus marks the field focused (the cursor is shown). Returns nil; it
// matches the bubbles signature so it fits in tea.Cmd chains.
func (e *Editor) Focus() tea.Cmd { e.init(); e.focused = true; return nil }

// Blur hides the cursor and the selection.
func (e *Editor) Blur() { e.focused = false }

// Focused reports whether the field has the cursor.
func (e *Editor) Focused() bool { return e.focused }

// SetWidth sets the width in cells.
func (e *Editor) SetWidth(w int) {
	if w < 2 {
		w = 2
	}
	if w != e.w {
		e.w = w
		e.rowsW = -1
	}
}

// SetHeight sets the number of rows shown (Multi).
func (e *Editor) SetHeight(h int) { e.h = max(1, h) }

// Width / Height in cells.
func (e *Editor) Width() int  { return e.w }
func (e *Editor) Height() int { return e.h }

// ---- cursor ----

// Cursor is the cursor position.
func (e *Editor) Cursor() Pos { return e.cur }

// SetCursor moves the cursor (clamped) and drops the selection.
func (e *Editor) SetCursor(p Pos) {
	e.init()
	e.follow = true
	e.cur = e.clamp(p)
	e.sel = false
	e.goal = -1
	e.typing = false
}

// CursorEnd moves to the end of the text.
func (e *Editor) CursorEnd() {
	e.init()
	n := len(e.lines) - 1
	e.SetCursor(Pos{n, len(e.lines[n])})
}

// CursorStart moves to the start of the text.
func (e *Editor) CursorStart() { e.SetCursor(Pos{}) }

// LineCount is the number of text lines.
func (e *Editor) LineCount() int { e.init(); return len(e.lines) }

func (e *Editor) clamp(p Pos) Pos {
	if p.Line < 0 {
		return Pos{}
	}
	if p.Line >= len(e.lines) {
		n := len(e.lines) - 1
		return Pos{n, len(e.lines[n])}
	}
	p.Col = max(0, min(p.Col, len(e.lines[p.Line])))
	return p
}

// AtFirstRow / AtLastRow: the cursor is on the first / last visual row
// (↑ / ↓ would leave the field).
func (e *Editor) AtFirstRow() bool { return e.rowOf(e.cur) == 0 }
func (e *Editor) AtLastRow() bool  { return e.rowOf(e.cur) == len(e.layout())-1 }

// ---- selection ----

// HasSelection reports whether some text is selected.
func (e *Editor) HasSelection() bool { return e.sel && e.cur != e.anchor }

// SelectionRange returns the selected range, start first.
func (e *Editor) SelectionRange() (Pos, Pos) {
	if !e.HasSelection() {
		return e.cur, e.cur
	}
	if e.cur.less(e.anchor) {
		return e.cur, e.anchor
	}
	return e.anchor, e.cur
}

// Selection is the selected text ("" for none, and always "" in a password field).
func (e *Editor) Selection() string {
	if !e.HasSelection() || e.Mask != 0 {
		return ""
	}
	a, b := e.SelectionRange()
	return e.textRange(a, b)
}

func (e *Editor) textRange(a, b Pos) string {
	if a.Line == b.Line {
		return string(e.lines[a.Line][a.Col:b.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(e.lines[a.Line][a.Col:]))
	for l := a.Line + 1; l < b.Line; l++ {
		sb.WriteByte('\n')
		sb.WriteString(string(e.lines[l]))
	}
	sb.WriteByte('\n')
	sb.WriteString(string(e.lines[b.Line][:b.Col]))
	return sb.String()
}

// SelectAll selects the whole text.
func (e *Editor) SelectAll() {
	e.init()
	n := len(e.lines) - 1
	e.anchor = Pos{}
	e.cur = Pos{n, len(e.lines[n])}
	e.sel = e.cur != e.anchor
	e.typing = false
}

// Select sets a selection from a to b (b is where the cursor goes).
func (e *Editor) Select(a, b Pos) {
	e.init()
	e.follow = true
	e.anchor, e.cur = e.clamp(a), e.clamp(b)
	e.sel = e.anchor != e.cur
	e.goal = -1
	e.typing = false
}

// ClearSelection drops the selection, keeping the cursor.
func (e *Editor) ClearSelection() { e.sel = false }

// ---- editing ----

func (e *Editor) save() {
	s := snap{e.Value(), e.cur, e.anchor, e.sel}
	if n := len(e.undo); n > 0 && e.undo[n-1].text == s.text {
		return
	}
	e.undo = append(e.undo, s)
	if len(e.undo) > maxUndo {
		e.undo = append(e.undo[:0], e.undo[len(e.undo)-maxUndo:]...)
	}
	e.redo = nil
}

func (e *Editor) restore(s snap) {
	e.setText(s.text)
	e.cur, e.anchor, e.sel = e.clamp(s.cur), e.clamp(s.anchor), s.sel
	e.goal = -1
}

// Undo reverts the last change; reports whether there was one.
func (e *Editor) Undo() bool {
	if len(e.undo) == 0 {
		return false
	}
	e.redo = append(e.redo, snap{e.Value(), e.cur, e.anchor, e.sel})
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.restore(s)
	e.typing = false
	return true
}

// Redo re-applies an undone change.
func (e *Editor) Redo() bool {
	if len(e.redo) == 0 {
		return false
	}
	e.undo = append(e.undo, snap{e.Value(), e.cur, e.anchor, e.sel})
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.restore(s)
	e.typing = false
	return true
}

func (e *Editor) count() int {
	n := len(e.lines) - 1
	for _, l := range e.lines {
		n += len(l)
	}
	return n
}

// deleteRange removes [a,b) and puts the cursor at a.
func (e *Editor) deleteRange(a, b Pos) {
	if a == b {
		return
	}
	first := e.lines[a.Line][:a.Col:a.Col]
	rest := e.lines[b.Line][b.Col:]
	line := append(append([]rune{}, first...), rest...)
	e.lines = append(e.lines[:a.Line+1], e.lines[b.Line+1:]...)
	e.lines[a.Line] = line
	e.cur, e.sel = a, false
	e.changed()
}

func (e *Editor) deleteSelection() bool {
	if !e.HasSelection() {
		e.sel = false
		return false
	}
	a, b := e.SelectionRange()
	e.deleteRange(a, b)
	return true
}

// insertRaw inserts already normalised text at the cursor.
func (e *Editor) insertRaw(s string) {
	parts := strings.Split(s, "\n")
	l := e.lines[e.cur.Line]
	head := append([]rune{}, l[:e.cur.Col]...)
	tail := append([]rune{}, l[e.cur.Col:]...)
	if len(parts) == 1 {
		r := []rune(parts[0])
		e.lines[e.cur.Line] = append(append(head, r...), tail...)
		e.cur.Col += len(r)
	} else {
		nl := make([][]rune, 0, len(parts))
		nl = append(nl, append(head, []rune(parts[0])...))
		for _, p := range parts[1 : len(parts)-1] {
			nl = append(nl, []rune(p))
		}
		last := []rune(parts[len(parts)-1])
		nl = append(nl, append(append([]rune{}, last...), tail...))
		lines := make([][]rune, 0, len(e.lines)+len(parts)-1)
		lines = append(lines, e.lines[:e.cur.Line]...)
		lines = append(lines, nl...)
		lines = append(lines, e.lines[e.cur.Line+1:]...)
		e.lines = lines
		e.cur = Pos{e.cur.Line + len(parts) - 1, len(last)}
	}
	e.sel = false
	e.changed()
}

// Insert puts text (a paste) at the cursor, replacing the selection. The
// text is cleaned for this kind of field (see Normalize) and shortened to
// the field's limit; cut reports that it had to be shortened.
func (e *Editor) Insert(s string) (cut bool) {
	e.init()
	e.follow = true
	s = Normalize(s, e.Kind)
	// address fields: pasted addresses are kept apart from the ones around
	// them with ", " (and never get a doubled separator)
	if e.Kind == Address && s != "" {
		a, b := e.cur, e.cur
		if e.HasSelection() {
			a, b = e.SelectionRange()
		}
		before := strings.TrimRight(string(e.lines[0][:a.Col]), " ")
		after := strings.TrimLeft(string(e.lines[0][b.Col:]), " ")
		looks := strings.Contains(s, "@")
		switch {
		case strings.HasSuffix(before, ",") || strings.HasSuffix(before, ";"):
			s = strings.TrimLeft(s, ", ")
			if string(e.lines[0][:a.Col]) == before {
				s = " " + s
			}
		case before != "" && looks && strings.Contains(before, "@"):
			s = ", " + s
		}
		if after != "" && looks && !strings.HasPrefix(after, ",") && !strings.HasPrefix(after, ";") {
			s += ", "
		}
	}
	limit := MaxLine
	if e.multi() {
		limit = MaxBody
	}
	if e.CharLimit > 0 {
		limit = min(limit, e.CharLimit)
	}
	have := e.count()
	if e.HasSelection() {
		a, b := e.SelectionRange()
		have -= len([]rune(e.textRange(a, b)))
	}
	room := max(0, limit-have)
	if r := []rune(s); len(r) > room {
		s, cut = string(r[:room]), true
	}
	if s == "" && !e.HasSelection() {
		return cut
	}
	e.save()
	e.typing = false
	e.deleteSelection()
	e.insertRaw(s)
	e.goal = -1
	return cut
}

func (e *Editor) typeRunes(rs []rune) bool {
	if e.CharLimit > 0 && e.count()+len(rs) > e.CharLimit && !e.HasSelection() {
		return false
	}
	brk := len(rs) != 1 || unicode.IsSpace(rs[0]) || e.HasSelection()
	if !e.typing || brk {
		e.save()
	}
	e.deleteSelection()
	e.insertRaw(string(rs))
	e.typing = !brk
	e.goal = -1
	return true
}

// ---- movement ----

func cellW(r rune) int {
	if r >= 0x20 && r < 0x7f {
		return 1
	}
	if r < 0x2000 && r >= 0xa0 && !(r >= 0x300 && r < 0x370) {
		return 1 // Latin (Turkish ğüşıöç, İ …), Greek, Cyrillic
	}
	c := grid.Clean(string(r))
	if c == "" {
		if visible(r) {
			return 1 // drawn as a small box (e.g. emoji)
		}
		return 0
	}
	if c == " " {
		return 1
	}
	return grid.Width(c)
}

// visible: a character that has a shape but the grid would drop (emoji,
// pictographs); zero-width and combining marks are not.
func visible(r rune) bool {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cc, r) {
		return false
	}
	if r >= 0xfe00 && r <= 0xfe0f || r >= 0xe0000 && r <= 0xe01ef || r >= 0x1f3fb && r <= 0x1f3ff {
		return false
	}
	return unicode.IsGraphic(r)
}

func (e *Editor) left(p Pos) Pos {
	if p.Col > 0 {
		p.Col--
		for p.Col > 0 && cellW(e.lines[p.Line][p.Col]) == 0 {
			p.Col--
		}
		return p
	}
	if p.Line > 0 {
		return Pos{p.Line - 1, len(e.lines[p.Line-1])}
	}
	return p
}

func (e *Editor) right(p Pos) Pos {
	l := e.lines[p.Line]
	if p.Col < len(l) {
		p.Col++
		for p.Col < len(l) && cellW(l[p.Col]) == 0 {
			p.Col++
		}
		return p
	}
	if p.Line < len(e.lines)-1 {
		return Pos{p.Line + 1, 0}
	}
	return p
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func (e *Editor) wordLeft(p Pos) Pos {
	if p.Col == 0 {
		return e.left(p)
	}
	l := e.lines[p.Line]
	c := p.Col
	for c > 0 && !isWord(l[c-1]) {
		c--
	}
	for c > 0 && isWord(l[c-1]) {
		c--
	}
	return Pos{p.Line, c}
}

func (e *Editor) wordRight(p Pos) Pos {
	l := e.lines[p.Line]
	if p.Col >= len(l) {
		return e.right(p)
	}
	c := p.Col
	for c < len(l) && isWord(l[c]) {
		c++
	}
	for c < len(l) && !isWord(l[c]) {
		c++
	}
	return Pos{p.Line, c}
}

// WordAt returns the word (or the single non-word character) around p.
func (e *Editor) WordAt(p Pos) (Pos, Pos) {
	e.init()
	p = e.clamp(p)
	l := e.lines[p.Line]
	if len(l) == 0 {
		return p, p
	}
	c := min(p.Col, len(l)-1)
	if !isWord(l[c]) {
		return Pos{p.Line, c}, Pos{p.Line, c + 1}
	}
	a, b := c, c
	for a > 0 && isWord(l[a-1]) {
		a--
	}
	for b < len(l) && isWord(l[b]) {
		b++
	}
	return Pos{p.Line, a}, Pos{p.Line, b}
}

// LineAt returns the whole text line around p.
func (e *Editor) LineAt(p Pos) (Pos, Pos) {
	e.init()
	p = e.clamp(p)
	return Pos{p.Line, 0}, Pos{p.Line, len(e.lines[p.Line])}
}

// ---- layout (visual rows) ----

func (e *Editor) wrapW() int {
	if !e.multi() {
		return 1 << 30
	}
	return max(1, e.w-1) // one cell kept free for the cursor at a row end
}

func (e *Editor) layout() []vrow {
	if e.rowsW == e.w && e.rows != nil {
		return e.rows
	}
	e.rows = e.rows[:0]
	ww := e.wrapW()
	for li, l := range e.lines {
		start := 0
		for {
			end, x, lastSpace := start, 0, -1
			for end < len(l) {
				cw := cellW(l[end])
				if x+cw > ww {
					break
				}
				x += cw
				if l[end] == ' ' {
					lastSpace = end
				}
				end++
			}
			if end >= len(l) {
				e.rows = append(e.rows, vrow{li, start, len(l)})
				break
			}
			if lastSpace >= start {
				end = lastSpace + 1 // break after the last space
			}
			if end == start {
				end = start + 1
			}
			e.rows = append(e.rows, vrow{li, start, end})
			start = end
		}
	}
	e.rowsW = e.w
	return e.rows
}

func (e *Editor) rowOf(p Pos) int {
	rows := e.layout()
	for i, r := range rows {
		if r.line == p.Line && p.Col >= r.start && (p.Col < r.end || r.end == len(e.lines[r.line]) && p.Col == r.end) {
			return i
		}
		if r.line > p.Line {
			return max(0, i-1)
		}
	}
	return len(rows) - 1
}

func (e *Editor) cellsTo(line []rune, from, to int) int {
	x := 0
	for i := from; i < to && i < len(line); i++ {
		x += cellW(line[i])
	}
	return x
}

func (e *Editor) colAtCell(r vrow, x int) int {
	l := e.lines[r.line]
	c, cx := r.start, 0
	for c < r.end {
		cw := cellW(l[c])
		if cx+cw > x {
			break
		}
		cx += cw
		c++
	}
	// a soft-wrapped row ends before its last character
	if c == r.end && r.end < len(l) && c > r.start {
		c--
	}
	return c
}

func (e *Editor) vertical(p Pos, d int) Pos {
	rows := e.layout()
	i := e.rowOf(p)
	r := rows[i]
	x := e.goal
	if x < 0 {
		x = e.cellsTo(e.lines[r.line], r.start, p.Col)
		e.goal = x
	}
	j := max(0, min(len(rows)-1, i+d))
	if j == i {
		if d < 0 {
			return Pos{r.line, r.start}
		}
		return Pos{r.line, r.end}
	}
	t := rows[j]
	return Pos{t.line, e.colAtCell(t, x)}
}

// ---- keys ----

// Result tells the caller what a key did.
type Result struct {
	Handled bool   // the field used the key
	Changed bool   // the text changed
	Copy    string // put this on the clipboard (Ctrl+C / Ctrl+X)
	Paste   bool   // Ctrl+V: read the clipboard and call Insert
	Cut     bool   // a paste had to be shortened
}

// Key handles one key press.
func (e *Editor) Key(k tea.KeyMsg) Result {
	e.init()
	e.follow = true
	before := e.Value()
	res := e.key(k)
	if e.Value() != before {
		res.Changed = true
	}
	return res
}

func (e *Editor) key(k tea.KeyMsg) Result {
	ok := Result{Handled: true}
	if k.Paste {
		ok.Cut = e.Insert(string(k.Runes))
		return ok
	}
	if k.Type == tea.KeyRunes && !k.Alt {
		rs := k.Runes[:0:0]
		for _, r := range k.Runes {
			if Printable(r) {
				rs = append(rs, r)
			}
		}
		if len(rs) > 0 { // NUL / control / private-use keys are never text
			e.typeRunes(rs)
		}
		return ok
	}
	key := k.String()
	move := func(to Pos, extend bool, keepGoal bool) Result {
		if !keepGoal {
			e.goal = -1
		}
		if extend {
			if !e.sel {
				e.anchor, e.sel = e.cur, true
			}
		} else {
			e.sel = false
		}
		e.cur = to
		e.typing = false
		return ok
	}
	collapse := func(toStart bool) bool {
		if !e.HasSelection() {
			e.sel = false
			return false
		}
		a, b := e.SelectionRange()
		if toStart {
			e.cur = a
		} else {
			e.cur = b
		}
		e.sel, e.goal = false, -1
		return true
	}
	lineEnd := func(p Pos) Pos { return Pos{p.Line, len(e.lines[p.Line])} }
	docEnd := func() Pos { n := len(e.lines) - 1; return Pos{n, len(e.lines[n])} }
	switch key {
	case " ", "space":
		e.typeRunes([]rune{' '})
	case "enter":
		if !e.multi() {
			return Result{}
		}
		e.typeRunes([]rune{'\n'})
	case "tab":
		return Result{} // fields use Tab to move on
	case "backspace", "ctrl+h":
		if e.HasSelection() {
			e.save()
			e.deleteSelection()
		} else if p := e.left(e.cur); p != e.cur {
			if !e.typing {
				e.save()
			}
			e.deleteRange(p, e.cur)
			e.typing = true
		}
	case "delete", "ctrl+d":
		if e.HasSelection() {
			e.save()
			e.deleteSelection()
		} else if p := e.right(e.cur); p != e.cur {
			e.save()
			e.deleteRange(e.cur, p)
		}
	case "alt+backspace", "ctrl+backspace":
		e.save()
		if !e.deleteSelection() {
			e.deleteRange(e.wordLeft(e.cur), e.cur)
		}
	case "ctrl+u": // delete to the start of the line
		e.save()
		if !e.deleteSelection() {
			e.deleteRange(Pos{e.cur.Line, 0}, e.cur)
		}
	case "ctrl+k": // delete to the end of the line
		e.save()
		if !e.deleteSelection() {
			e.deleteRange(e.cur, lineEnd(e.cur))
		}
	case "left":
		if !collapse(true) {
			return move(e.left(e.cur), false, false)
		}
	case "right":
		if !collapse(false) {
			return move(e.right(e.cur), false, false)
		}
	case "shift+left":
		return move(e.left(e.cur), true, false)
	case "shift+right":
		return move(e.right(e.cur), true, false)
	case "ctrl+left", "alt+left", "alt+b":
		return move(e.wordLeft(e.cur), false, false)
	case "ctrl+right", "alt+right", "alt+f":
		return move(e.wordRight(e.cur), false, false)
	case "ctrl+shift+left":
		return move(e.wordLeft(e.cur), true, false)
	case "ctrl+shift+right":
		return move(e.wordRight(e.cur), true, false)
	case "home":
		return move(Pos{e.cur.Line, 0}, false, false)
	case "end":
		return move(lineEnd(e.cur), false, false)
	case "shift+home":
		return move(Pos{e.cur.Line, 0}, true, false)
	case "shift+end":
		return move(lineEnd(e.cur), true, false)
	case "ctrl+home":
		return move(Pos{}, false, false)
	case "ctrl+end":
		return move(docEnd(), false, false)
	case "ctrl+shift+home":
		return move(Pos{}, true, false)
	case "ctrl+shift+end":
		return move(docEnd(), true, false)
	case "up", "down", "shift+up", "shift+down", "pgup", "pgdown", "shift+pgup", "shift+pgdown", "ctrl+shift+up", "ctrl+shift+down":
		if !e.multi() {
			if strings.HasPrefix(key, "shift+") || strings.HasPrefix(key, "ctrl+shift+") {
				if strings.HasSuffix(key, "up") {
					return move(Pos{}, true, false)
				}
				return move(docEnd(), true, false)
			}
			return Result{}
		}
		d := 1
		if strings.HasSuffix(key, "up") {
			d = -1
		}
		if strings.Contains(key, "pg") {
			d *= max(1, e.h-1)
		}
		extend := strings.Contains(key, "shift+")
		if !extend && e.HasSelection() {
			collapse(d < 0)
		}
		return move(e.vertical(e.cur, d), extend, true)
	case "ctrl+a":
		e.SelectAll()
	case "ctrl+c":
		ok.Copy = e.Selection()
	case "ctrl+x":
		if s := e.Selection(); s != "" {
			e.save()
			e.deleteSelection()
			ok.Copy = s
		}
	case "ctrl+v":
		ok.Paste = true
	case "ctrl+z":
		e.Undo()
	case "ctrl+y":
		e.Redo()
	case "esc":
		if e.HasSelection() {
			e.sel = false
			return ok
		}
		return Result{}
	default:
		return Result{}
	}
	return ok
}

// ---- mouse ----

// PosAt is the text position under the cell (x, y) relative to the field's
// top-left corner (as last drawn).
func (e *Editor) PosAt(x, y int) Pos {
	e.init()
	if !e.multi() {
		l := e.lines[0]
		c, cx := 0, -e.off
		for c < len(l) {
			cw := cellW(l[c])
			if e.Mask != 0 {
				cw = 1
			}
			if cx+cw > x {
				if x-cx >= (cw+1)/2 && cw > 1 {
					c++
				}
				break
			}
			cx += cw
			c++
		}
		return Pos{0, c}
	}
	rows := e.layout()
	i := max(0, min(len(rows)-1, e.top+y))
	if e.top+y < 0 {
		return Pos{}
	}
	if e.top+y >= len(rows) {
		r := rows[len(rows)-1]
		return Pos{r.line, len(e.lines[r.line])}
	}
	return Pos{rows[i].line, e.colAtCell(rows[i], max(0, x))}
}

// Click puts the cursor at (x, y); extend keeps the selection anchor (Shift+click / drag).
func (e *Editor) Click(x, y int, extend bool) {
	p := e.PosAt(x, y)
	if extend {
		if !e.sel {
			e.anchor, e.sel = e.cur, true
		}
		e.cur = p
	} else {
		e.cur, e.anchor, e.sel = p, p, false
	}
	e.goal = -1
	e.typing = false
	e.follow = true
}

// ScrollBy scrolls a Multi field by d rows (the mouse wheel / drag at an edge).
func (e *Editor) ScrollBy(d int) {
	if e.multi() {
		e.top = max(0, min(e.top+d, len(e.layout())-e.h))
	}
}

// ---- drawing ----

// Style for Draw.
type Style struct {
	FG, BG, Faint, Cursor, SelBG string
}

// Draw paints the field at (x, y), w×h cells (h is 1 for one-line fields).
func (e *Editor) Draw(g *grid.Grid, x, y, w, h int, st Style) {
	e.init()
	e.SetWidth(w)
	if e.multi() {
		e.SetHeight(h)
	}
	g.Fill(x, y, w, max(1, h), st.BG)
	if e.Value() == "" {
		if e.Placeholder != "" {
			g.Put(x, y, e.Placeholder, grid.Style{FG: st.Faint, BG: st.BG}, w-1)
		}
		if e.focused {
			put(g, x, y, ' ', grid.Style{FG: st.BG, BG: st.Cursor})
		}
		return
	}
	a, b := e.SelectionRange()
	showSel := e.HasSelection()
	cell := func(cx, cy int, li, col int, r rune) {
		s := grid.Style{FG: st.FG, BG: st.BG}
		pos := Pos{li, col}
		if showSel && !pos.less(a) && pos.less(b) {
			s.BG = st.SelBG
		}
		if e.focused && pos == e.cur {
			s = grid.Style{FG: st.BG, BG: st.Cursor}
		}
		put(g, cx, cy, r, s)
	}
	if !e.multi() {
		l := e.lines[0]
		curX := 0
		for i := 0; i < e.cur.Col && i < len(l); i++ {
			curX += e.cellOf(l[i])
		}
		total := curX
		for i := e.cur.Col; i < len(l); i++ {
			total += e.cellOf(l[i])
		}
		if curX < e.off {
			e.off = curX
		}
		if curX >= e.off+w {
			e.off = curX - w + 1
		}
		if total+1 <= w {
			e.off = 0
		}
		cx := -e.off
		for i, r := range l {
			cw := e.cellOf(r)
			if cx >= 0 && cx+cw <= w && cw > 0 {
				cell(x+cx, y, 0, i, e.shown(r))
			}
			cx += cw
			if cx >= w {
				break
			}
		}
		if e.focused && e.cur.Col == len(l) && curX-e.off < w {
			put(g, x+curX-e.off, y, ' ', grid.Style{FG: st.BG, BG: st.Cursor})
		}
		return
	}
	rows := e.layout()
	ci := e.rowOf(e.cur)
	if e.focused && e.follow {
		if ci < e.top {
			e.top = ci
		}
		if ci >= e.top+h {
			e.top = ci - h + 1
		}
	}
	e.follow = false
	e.top = max(0, min(e.top, max(0, len(rows)-h)))
	for vy := 0; vy < h && e.top+vy < len(rows); vy++ {
		r := rows[e.top+vy]
		l := e.lines[r.line]
		cx := 0
		for c := r.start; c < r.end; c++ {
			cw := cellW(l[c])
			if cw == 0 {
				continue
			}
			cell(x+cx, y+vy, r.line, c, e.shown(l[c]))
			cx += cw
		}
		// a selected line break shows as one highlighted cell
		end := Pos{r.line, r.end}
		if r.end == len(l) {
			if showSel && !end.less(a) && end.less(b) && cx < w {
				put(g, x+cx, y+vy, ' ', grid.Style{FG: st.FG, BG: st.SelBG})
			}
			if e.focused && e.cur == end && cx < w {
				put(g, x+cx, y+vy, ' ', grid.Style{FG: st.BG, BG: st.Cursor})
			}
		}
	}
}

func (e *Editor) cellOf(r rune) int {
	if e.Mask != 0 {
		return 1
	}
	return cellW(r)
}

func (e *Editor) shown(r rune) rune {
	if e.Mask != 0 {
		return e.Mask
	}
	if r >= 0x20 && r < 0x7f {
		return r
	}
	if grid.Clean(string(r)) == "" {
		return '□'
	}
	return r
}

func put(g *grid.Grid, x, y int, r rune, s grid.Style) {
	if r == '\t' {
		r = ' '
	}
	g.PutClean(x, y, string(r), s)
}

// ---- paste clean-up ----

// Normalize cleans text for a field: terminal escape codes and control
// characters go, line ends become "\n", tabs become four spaces, and
// letters are composed (NFC). One-line fields join lines with spaces;
// address fields turn line breaks and ";" into ", ".
func Normalize(s string, k Kind) string {
	if strings.ContainsRune(s, 0x1b) {
		s = ansi.Strip(s)
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = norm.NFC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r >= 0x7f && r < 0xa0:
		case r == 0xfeff || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 || r == 0x2028 || r == 0x2029:
			// BOM and text-direction overrides (they can disguise text); line/paragraph separators
			if r == 0x2028 || r == 0x2029 {
				b.WriteByte('\n')
			}
		default:
			b.WriteRune(r)
		}
	}
	s = b.String()
	switch k {
	case Single:
		if strings.Contains(s, "\n") {
			lead := strings.TrimLeft(s, " \n") != s // "\nmore": keep it apart from the text before
			var parts []string
			for _, p := range strings.Split(s, "\n") {
				if p = strings.TrimSpace(p); p != "" {
					parts = append(parts, p)
				}
			}
			s = strings.Join(parts, " ")
			if lead && s != "" {
				s = " " + s
			}
		}
	case Address:
		s = strings.NewReplacer("\n", ",", ";", ",").Replace(s)
		var parts []string
		for _, p := range strings.Split(s, ",") {
			if p = strings.Join(strings.Fields(p), " "); p != "" {
				parts = append(parts, p)
			}
		}
		s = strings.Join(parts, ", ")
	}
	return s
}

// Printable reports whether r may be typed into a field: no NUL, control,
// private-use or lone surrogate characters (some consoles send those for
// Ctrl / AltGr alone).
func Printable(r rune) bool {
	switch {
	case r < 0x20, r >= 0x7f && r < 0xa0, r >= 0xd800 && r <= 0xdfff:
		return false
	case unicode.Is(unicode.Co, r):
		return false
	}
	return r == ' ' || unicode.IsPrint(r)
}
