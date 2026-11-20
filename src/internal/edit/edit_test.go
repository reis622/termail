package edit

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/grid"
)

func typ(e *Editor, s string) {
	for _, r := range s {
		if r == '\n' {
			e.Key(tea.KeyMsg{Type: tea.KeyEnter})
			continue
		}
		e.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

var keyTypes = map[string]tea.KeyType{
	"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
	"home": tea.KeyHome, "end": tea.KeyEnd, "shift+left": tea.KeyShiftLeft, "shift+right": tea.KeyShiftRight,
	"shift+up": tea.KeyShiftUp, "shift+down": tea.KeyShiftDown, "shift+home": tea.KeyShiftHome, "shift+end": tea.KeyShiftEnd,
	"ctrl+shift+left": tea.KeyCtrlShiftLeft, "ctrl+shift+right": tea.KeyCtrlShiftRight, "ctrl+left": tea.KeyCtrlLeft,
	"backspace": tea.KeyBackspace, "delete": tea.KeyDelete, "ctrl+a": tea.KeyCtrlA, "ctrl+c": tea.KeyCtrlC,
	"ctrl+x": tea.KeyCtrlX, "ctrl+v": tea.KeyCtrlV, "ctrl+z": tea.KeyCtrlZ, "ctrl+y": tea.KeyCtrlY, "enter": tea.KeyEnter,
	"esc": tea.KeyEsc, "ctrl+home": tea.KeyCtrlHome, "ctrl+end": tea.KeyCtrlEnd,
}

func press(t *testing.T, e *Editor, keys ...string) Result {
	t.Helper()
	var r Result
	for _, k := range keys {
		kt, ok := keyTypes[k]
		if !ok {
			t.Fatalf("no key %q", k)
		}
		r = e.Key(tea.KeyMsg{Type: kt})
	}
	return r
}

func TestSelectionKeys(t *testing.T) {
	e := New(Single)
	e.Focus()
	typ(&e, "Merhaba dünya")
	press(t, &e, "shift+left", "shift+left", "shift+left", "shift+left", "shift+left")
	if got := e.Selection(); got != "dünya" {
		t.Fatalf("selection %q", got)
	}
	if r := press(t, &e, "ctrl+c"); r.Copy != "dünya" || !r.Handled || r.Changed {
		t.Fatalf("copy %+v", r)
	}
	typ(&e, "İstanbul") // typing replaces the selection
	if e.Value() != "Merhaba İstanbul" || e.HasSelection() {
		t.Fatalf("%q", e.Value())
	}
	press(t, &e, "ctrl+shift+left")
	if r := press(t, &e, "ctrl+x"); r.Copy != "İstanbul" || e.Value() != "Merhaba " {
		t.Fatalf("cut %+v %q", r, e.Value())
	}
	press(t, &e, "ctrl+z")
	if e.Value() != "Merhaba İstanbul" {
		t.Fatalf("undo cut: %q", e.Value())
	}
	press(t, &e, "ctrl+y")
	if e.Value() != "Merhaba " {
		t.Fatalf("redo: %q", e.Value())
	}
	press(t, &e, "ctrl+a")
	if e.Selection() != "Merhaba " {
		t.Fatal("select all")
	}
	press(t, &e, "backspace")
	if e.Value() != "" {
		t.Fatal("backspace on selection")
	}
	typ(&e, "abc")
	press(t, &e, "home", "shift+end")
	press(t, &e, "delete")
	if e.Value() != "" {
		t.Fatal("delete on selection")
	}
	// no selection: Ctrl+C copies nothing (and the caller must not quit)
	typ(&e, "x")
	if r := press(t, &e, "ctrl+c"); r.Copy != "" || !r.Handled {
		t.Fatalf("%+v", r)
	}
	if r := press(t, &e, "ctrl+v"); !r.Paste {
		t.Fatal("ctrl+v must ask for a paste")
	}
	// left/right collapse a selection to its ends
	press(t, &e, "shift+home", "right")
	if e.HasSelection() || e.Cursor().Col != 1 {
		t.Fatal("collapse")
	}
}

func TestPasswordNeverCopied(t *testing.T) {
	e := New(Single)
	e.Mask = '•'
	e.Insert("gizli şifre")
	press(t, &e, "ctrl+a")
	if r := press(t, &e, "ctrl+c"); r.Copy != "" {
		t.Fatal("password copied")
	}
	if r := press(t, &e, "ctrl+x"); r.Copy != "" || e.Value() != "gizli şifre" {
		t.Fatal("password cut")
	}
}

func TestMultiSelectionAndUndo(t *testing.T) {
	e := New(Multi)
	e.Focus()
	e.SetWidth(30)
	e.SetHeight(5)
	typ(&e, "birinci satır\nikinci satır\nüçüncü")
	press(t, &e, "ctrl+home", "shift+down", "shift+end")
	if got := e.Selection(); got != "birinci satır\nikinci satır" {
		t.Fatalf("%q", got)
	}
	typ(&e, "X")
	if e.Value() != "X\nüçüncü" {
		t.Fatalf("%q", e.Value())
	}
	press(t, &e, "ctrl+z")
	if e.Value() != "birinci satır\nikinci satır\nüçüncü" {
		t.Fatalf("undo: %q", e.Value())
	}
	// typing a word is one undo step
	press(t, &e, "ctrl+end")
	typ(&e, " kelime")
	press(t, &e, "ctrl+z")
	if e.Value() != "birinci satır\nikinci satır\nüçüncü " {
		t.Fatalf("word undo: %q", e.Value())
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		k    Kind
		want string
	}{
		{"a\r\nb\rc\n", Multi, "a\nb\nc\n"},
		{"tab\there", Multi, "tab    here"},
		{"zil\x07 \x1b[31mkırmızı\x1b[0m\x00", Multi, "zil kırmızı"},
		{"gizli\u202eters\u202c", Multi, "gizliters"},
		{"\ufeffbom", Single, "bom"},
		{"I\u0307", Multi, "İ"},
		{"satır bir\r\n  satır iki \n", Single, "satır bir satır iki"},
		{"tek  satır ", Single, "tek  satır "},
		{"a@b.com\nc@d.com;e@f.com;\n", Address, "a@b.com, c@d.com, e@f.com"},
		{"\"Kaya, Mehmet\" <m@k.com>; x@y.z", Address, "\"Kaya, Mehmet\" <m@k.com>, x@y.z"},
	}
	for _, c := range cases {
		if got := Normalize(c.in, c.k); got != c.want {
			t.Errorf("Normalize(%q, %d) = %q, want %q", c.in, c.k, got, c.want)
		}
	}
}

func TestPasteIntoFields(t *testing.T) {
	to := New(Address)
	to.Insert("ali@x.com")
	typ(&to, ", ")
	to.Insert("veli@y.com\r\nayşe@z.com")
	if to.Value() != "ali@x.com, veli@y.com, ayşe@z.com" {
		t.Fatalf("%q", to.Value())
	}
	to.Insert("can@w.com") // right after an address: a separator is added
	if to.Value() != "ali@x.com, veli@y.com, ayşe@z.com, can@w.com" {
		t.Fatalf("%q", to.Value())
	}
	to.SetCursor(Pos{0, 0}) // before the others
	to.Insert("ilk@a.com")
	if to.Value() != "ilk@a.com, ali@x.com, veli@y.com, ayşe@z.com, can@w.com" {
		t.Fatalf("%q", to.Value())
	}
	to.SetValue("ali@x.com,")
	to.Insert("b@c.com")
	if to.Value() != "ali@x.com, b@c.com" {
		t.Fatalf("%q", to.Value())
	}
	subj := New(Single)
	if subj.Insert("Toplantı\nnotları"); subj.Value() != "Toplantı notları" {
		t.Fatalf("%q", subj.Value())
	}
	if subj.Insert("\n ve kararlar\n"); subj.Value() != "Toplantı notları ve kararlar" {
		t.Fatalf("%q", subj.Value())
	}
	body := New(Multi)
	body.SetWidth(40)
	body.Insert("Merhaba,\r\n\r\nNasılsın?")
	if body.Value() != "Merhaba,\n\nNasılsın?" || body.LineCount() != 3 {
		t.Fatalf("%q", body.Value())
	}
	// a bracketed paste key arrives as one message
	body.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\nek satır"), Paste: true})
	if !strings.HasSuffix(body.Value(), "?\nek satır") {
		t.Fatalf("%q", body.Value())
	}
	// absurd sizes are cut and reported
	big := New(Single)
	if !big.Insert(strings.Repeat("a", MaxLine+50)) || len(big.Value()) != MaxLine {
		t.Fatal("one-line paste not capped")
	}
	lim := New(Single)
	lim.CharLimit = 5
	if !lim.Insert("1234567") || lim.Value() != "12345" {
		t.Fatal("char limit")
	}
	// paste replaces the selection, one undo step
	body.SelectAll()
	body.Insert("yeni")
	if body.Value() != "yeni" {
		t.Fatal("paste over selection")
	}
	body.Undo()
	if !strings.HasPrefix(body.Value(), "Merhaba,") {
		t.Fatal("undo paste")
	}
}

func draw(e *Editor, w, h int) []string {
	g := grid.New(w, h, "#fff", "#000")
	e.Draw(g, 0, 0, w, h, Style{FG: "#fff", BG: "#000", Faint: "#888", Cursor: "#0af", SelBG: "#334"})
	out := strings.Split(strings.TrimRight(stripRender(g), "\n"), "\n")
	return out
}

// stripRender gives the grid text (wide chars once, their right half skipped).
func stripRender(g *grid.Grid) string {
	var b strings.Builder
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			c := g.At(x, y)
			b.WriteString(c.Ch)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestWrapWidthsAndClicks(t *testing.T) {
	e := New(Multi)
	e.Focus()
	e.Insert("Çağrı İşık ğüşöç 日本語のテキスト end")
	rows := draw(&e, 11, 6) // 10 cells for text, 1 for the cursor
	want := []string{"Çağrı", "İşık", "ğüşöç", "日本語のテ", "キスト end"}
	for i, w := range want {
		if strings.TrimRight(rows[i], " ") != w {
			t.Fatalf("row %d = %q, want %q (all %q)", i, rows[i], w, rows)
		}
	}
	// a click on the right half of a wide character lands on that character
	p := e.PosAt(3, 3) // "日本語のテ": cells 2-3 are 本
	if string(e.lines[p.Line][p.Col]) != "本" {
		t.Fatalf("click on wide char: %+v", p)
	}
	p = e.PosAt(4, 1) // "İşık": cell 4 is the space after
	if p.Col != 10 {
		t.Fatalf("click on space: %+v", p)
	}
	// ↓ keeps the column across rows
	e.SetCursor(Pos{0, 2})
	press(t, &e, "down")
	if e.Cursor().Col != 8 { // İ ş ı …: col 6 + 2
		t.Fatalf("down: %+v", e.Cursor())
	}
	if !press(t, &e, "up").Handled || !e.AtFirstRow() {
		t.Fatal("first row")
	}
	press(t, &e, "ctrl+end")
	if !e.AtLastRow() {
		t.Fatal("last row")
	}
	// word / line selection (double / triple click)
	a, b := e.WordAt(Pos{0, 7})
	if string(e.lines[0][a.Col:b.Col]) != "İşık" {
		t.Fatalf("word %v %v", a, b)
	}
	e.Select(e.LineAt(Pos{0, 3}))
	if e.Selection() != e.Value() {
		t.Fatal("line")
	}
}

func TestSingleLineScrollAndMask(t *testing.T) {
	e := New(Single)
	e.Focus()
	e.Insert("abcdefghijklmnopqrstuvwxyz")
	rows := draw(&e, 10, 1)
	if !strings.HasPrefix(rows[0], "rstuvwxyz") {
		t.Fatalf("%q", rows[0])
	}
	e.SetCursor(Pos{0, 0})
	rows = draw(&e, 10, 1)
	if !strings.HasPrefix(rows[0], "abcdefghij") {
		t.Fatalf("%q", rows[0])
	}
	pw := New(Single)
	pw.Mask = '•'
	pw.Insert("şifre")
	if rows := draw(&pw, 10, 1); !strings.HasPrefix(rows[0], "•••••") {
		t.Fatalf("%q", rows[0])
	}
}

func TestEmojiShownAsBox(t *testing.T) {
	e := New(Single)
	e.Insert("hi 👍🏽!")
	if rows := draw(&e, 10, 1); !strings.HasPrefix(rows[0], "hi □!") {
		t.Fatalf("%q", rows[0])
	}
	e.SetCursor(Pos{0, 3})
	press(t, &e, "right") // skips the skin-tone modifier
	if e.Cursor().Col != 5 {
		t.Fatalf("%+v", e.Cursor())
	}
}
