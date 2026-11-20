package ui

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/edit"
	"termail/internal/mailmsg"
)

const para1 = "Merhaba Ahmet, yarınki toplantı için hazırladığım raporu ekte bulabilirsin; özellikle üçüncü bölümdeki rakamlara göz atmanı rica ederim. 日本語のテキストも少し入っています。 Görüşmek üzere."

const selMail = "From: =?utf-8?q?Ay=C5=9Fe_Kaya?= <ayse@ornek.com>\r\nTo: ben@test.local\r\nSubject: Rapor\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
	para1 + "\r\n\r\nİkinci paragraf kısa.\r\n"

// selModel: the selected mail (UID 3) has a long Turkish / Japanese paragraph.
func selModel(t *testing.T) (*Model, *[]string) {
	t.Helper()
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	m, a := testModel(t)
	a.Open = true
	p, err := mailmsg.Parse([]byte(selMail))
	if err != nil {
		t.Fatal(err)
	}
	m.bodies.Put(bodyKey{0, "INBOX", 3}, p)
	m.applySize(140, 40)
	clock := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	m.opt.Now = func() time.Time { return clock }
	return m, stubOpen(t)
}

func mouse(m *Model, x, y int, a tea.MouseAction, b tea.MouseButton) {
	m.Update(tea.MouseMsg{X: x, Y: y, Action: a, Button: b})
	m.View()
}

func copies(log *[]string) []string {
	var out []string
	for _, s := range *log {
		if c, ok := strings.CutPrefix(s, "copy:"); ok {
			out = append(out, c)
		}
	}
	return out
}

func takeCopy(m *Model) {
	m.copySelection()
}

func TestDragSelectCopiesOriginalText(t *testing.T) {
	m, log := selModel(t)
	x0, y0 := find(t, m, "Merhaba Ahmet")
	x1, y1 := find(t, m, "rica ederim")
	if y1 <= y0 {
		t.Fatalf("the paragraph should wrap (y %d → %d)", y0, y1)
	}
	mouse(m, x0, y0, tea.MouseActionPress, tea.MouseButtonLeft)
	mouse(m, x0+5, y0+1, tea.MouseActionMotion, tea.MouseButtonLeft) // wander
	mouse(m, x1+3, y1, tea.MouseActionMotion, tea.MouseButtonLeft)   // end on the last "a" of "rica"
	// highlighted while dragging
	if c := m.gridBuf.At(x0, y0); c.BG != cTextSel {
		t.Fatalf("start cell not highlighted: %q", c.BG)
	}
	if c := m.gridBuf.At(x1+4, y1); c.BG == cTextSel {
		t.Fatal("cell after the selection highlighted")
	}
	mouse(m, x1+3, y1, tea.MouseActionRelease, tea.MouseButtonLeft)
	if m.rsel == nil || !m.rsel.moved {
		t.Fatal("selection must stay visible after release")
	}
	if c := m.gridBuf.At(x0, y0); c.BG != cTextSel {
		t.Fatalf("selection highlight gone after release: %q", c.BG)
	}
	takeCopy(m)
	got := copies(log)
	want := para1[:strings.Index(para1, "rica")+4]
	if len(got) != 1 || got[0] != want {
		t.Fatalf("copied %q\nwant   %q", got, want)
	}
	if !hasToast(m, "Kopyalandı") {
		t.Fatal("no toast")
	}
	// Ctrl+C copies the selection again (and does not quit)
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.quitting || len(copies(log)) != 2 {
		t.Fatal("Ctrl+C with a selection must copy, not quit")
	}
	// right-click in the text copies too
	mouse(m, x0+2, y0, tea.MouseActionPress, tea.MouseButtonRight)
	if len(copies(log)) != 3 {
		t.Fatal("right-click did not copy")
	}
	// Esc clears it; a plain click too
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.View()
	if m.rsel != nil || m.focus != focusReader {
		t.Fatal("Esc should drop the selection first (and stay in the mail)")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.quitting {
		t.Fatal("Ctrl+C with nothing selected (outside fields) quits as before")
	}
}

func TestDragAcrossParagraphsAndBackwards(t *testing.T) {
	m, log := selModel(t)
	x0, y0 := find(t, m, "Görüşmek üzere")
	x1, y1 := find(t, m, "İkinci paragraf")
	// drag backwards: from "kısa" up to "Görüşmek"
	mouse(m, x1+len("İkinci paragraf kısa")-3, y1, tea.MouseActionPress, tea.MouseButtonLeft)
	mouse(m, x0, y0, tea.MouseActionMotion, tea.MouseButtonLeft)
	mouse(m, x0, y0, tea.MouseActionRelease, tea.MouseButtonLeft)
	takeCopy(m)
	got := copies(log)
	if len(got) != 1 || got[0] != "Görüşmek üzere.\n\nİkinci paragraf kısa" {
		t.Fatalf("%q", got)
	}
}

func TestDoubleAndTripleClick(t *testing.T) {
	m, log := selModel(t)
	x, y := find(t, m, "テキスト")
	for i := 0; i < 2; i++ {
		mouse(m, x+1, y, tea.MouseActionPress, tea.MouseButtonLeft) // right half of テ
		mouse(m, x+1, y, tea.MouseActionRelease, tea.MouseButtonLeft)
	}
	takeCopy(m)
	if got := copies(log); len(got) != 1 || got[0] != "日本語のテキストも少し入っています" {
		t.Fatalf("double click: %q", got)
	}
	x, y = find(t, m, "toplantı")
	for i := 0; i < 2; i++ {
		mouse(m, x+2, y, tea.MouseActionPress, tea.MouseButtonLeft)
		mouse(m, x+2, y, tea.MouseActionRelease, tea.MouseButtonLeft)
	}
	takeCopy(m)
	if got := copies(log); got[len(got)-1] != "toplantı" {
		t.Fatalf("double click: %q", got)
	}
	mouse(m, x+2, y, tea.MouseActionPress, tea.MouseButtonLeft)
	mouse(m, x+2, y, tea.MouseActionRelease, tea.MouseButtonLeft)
	takeCopy(m)
	line := copies(log)[len(copies(log))-1]
	if !strings.Contains(line, "toplantı") {
		t.Fatalf("triple click line missing toplantı: %q", line)
	}
	if line == para1 {
		t.Fatal("triple click must select the line, not the whole paragraph")
	}
}

func TestPlainClickDoesNotCopy(t *testing.T) {
	m, log := selModel(t)
	x, y := find(t, m, "Merhaba")
	click(m, x, y)
	if len(copies(log)) != 0 || m.rsel != nil {
		t.Fatal("a click copied / left a selection")
	}
	if m.focus != focusReader {
		t.Fatal("a click in the reading pane steps into the mail (as before)")
	}
}

func TestCopyKeys(t *testing.T) {
	m, log := selModel(t)
	m.View()
	press(m, "c")
	press(m, "C")
	got := copies(log)
	if len(got) != 2 || !strings.HasPrefix(got[0], "Merhaba Ahmet") || !strings.HasSuffix(got[0], "kısa.\n") && !strings.HasSuffix(got[0], "kısa.") || got[1] != "ayse@ornek.com" {
		t.Fatalf("%q", got)
	}
	if !hasToast(m, "Adres kopyalandı") {
		t.Fatal("toast")
	}
}

func TestWrapSpansMapBack(t *testing.T) {
	words := []string{"a", "kelime", "İstanbul", "日本語", "çok-uzun-bir-bağlantı-https://ornek.com/yol/yol/yol", "", "> alıntı", ">>", "x\u0301", "\t"}
	rnd := rand.New(rand.NewSource(7))
	for n := 0; n < 3000; n++ {
		var b strings.Builder
		for i := rnd.Intn(30); i > 0; i-- {
			b.WriteString(words[rnd.Intn(len(words))])
			if rnd.Intn(8) == 0 {
				b.WriteString("\n")
			} else {
				b.WriteString(strings.Repeat(" ", 1+rnd.Intn(2)))
			}
		}
		w := 4 + rnd.Intn(30)
		lines, paras := wrapSpans(b.String(), w)
		texts := wrapText(b.String(), w)
		for i, l := range lines {
			if l.text != texts[i] {
				t.Fatal("wrapText changed")
			}
			pp := paras[l.para]
			if l.pre > len(l.text) || l.start < 0 || l.end > len(pp) || l.start > l.end || l.text[l.pre:] != pp[l.start:l.end] {
				t.Fatalf("w=%d %q: line %q pre %d start %d end %d", w, b.String(), l.text, l.pre, l.start, l.end)
			}
		}
	}
}

func TestCtrlCInFieldsNeverQuits(t *testing.T) {
	m, _, c := newCompose(t)
	log := stubOpen(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlC}) // nothing selected: nothing happens
	if m.quitting || len(copies(log)) != 0 {
		t.Fatal("Ctrl+C in compose quit / copied")
	}
	c.focus = fBody
	c.applyFocus()
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlA}) // selects the body, no account picker
	if m.acctPicker != nil || c.body.Selection() != "Selam,\nşuna bir bak." {
		t.Fatalf("Ctrl+A: picker=%v sel=%q", m.acctPicker != nil, c.body.Selection())
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.quitting || len(copies(log)) != 1 || copies(log)[0] != "Selam,\nşuna bir bak." || !hasToast(m, "Kopyalandı") {
		t.Fatal("Ctrl+C did not copy the selection")
	}
	if c.status != "Kopyalandı" { // toasts are hidden on compose tabs: the note goes to the status line
		t.Fatalf("compose status = %q", c.status)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	if c.body.Value() != "" || copies(log)[1] != "Selam,\nşuna bir bak." {
		t.Fatal("Ctrl+X")
	}
	if c.status != "Kopyalandı" {
		t.Fatalf("cut note = %q", c.status)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if c.status != "" {
		t.Fatalf("note not cleared by the next key: %q", c.status)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlY})
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if c.body.Value() != "Selam,\nşuna bir bak." {
		t.Fatal("Ctrl+Z")
	}
	// Shift+arrows, typing replaces the selection
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlEnd}) // (undo brought the selection back)
	m.onKey(tea.KeyMsg{Type: tea.KeyShiftLeft})
	m.onKey(tea.KeyMsg{Type: tea.KeyShiftLeft})
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if c.body.Value() != "Selam,\nşuna bir ba?" {
		t.Fatalf("%q", c.body.Value())
	}
	// Esc with a selection only drops the selection
	m.onKey(tea.KeyMsg{Type: tea.KeyShiftHome})
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.tabs) != 2 || c.body.HasSelection() {
		t.Fatal("Esc with a selection closed the tab")
	}

	// search box
	m2, _ := memModel(t, 3)
	press(m2, "/")
	m2.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ileti")})
	m2.onKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m2.quitting {
		t.Fatal("Ctrl+C in the search box quit")
	}
	// sign-in screen
	m3, _ := memModel(t, 1)
	m3.login = newLogin(m3, true)
	m3.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ad@ornek.com")})
	m3.onKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	m3.onKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m3.quitting || copies(log)[len(copies(log))-1] != "ad@ornek.com" {
		t.Fatal("Ctrl+C on the sign-in screen")
	}
	// outside text fields Ctrl+A is still the account picker
	m4, _ := memModel(t, 1)
	m4.onKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	if m4.acctPicker == nil {
		t.Fatal("Ctrl+A account picker outside fields")
	}
}

func stubClipboard(t *testing.T, s *string) {
	old := readClipboardFn
	readClipboardFn = func() (string, error) { return *s, nil }
	t.Cleanup(func() { readClipboardFn = old })
}

func TestPasteAndDedupe(t *testing.T) {
	m, _, c := newCompose(t)
	clock := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	m.opt.Now = func() time.Time { return clock }
	clip := "veli@y.com\r\nayşe@z.com;"
	stubClipboard(t, &clip)
	c.focus = fTo
	c.applyFocus()
	c.to.CursorEnd()
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(", ")})
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if c.to.Value() != "arthur@gmail.com, veli@y.com, ayşe@z.com" {
		t.Fatalf("Kime paste: %q", c.to.Value())
	}
	// the terminal sends the same text as a bracketed paste right after: dropped
	clock = clock.Add(80 * time.Millisecond)
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("veli@y.com\nayşe@z.com;"), Paste: true})
	if c.to.Value() != "arthur@gmail.com, veli@y.com, ayşe@z.com" {
		t.Fatalf("double paste: %q", c.to.Value())
	}
	// other order: bracketed first, then Ctrl+V
	c.focus = fSubject
	c.applyFocus()
	c.subject.SetValue("")
	clock = clock.Add(5 * time.Second)
	clip = "Toplantı\r\nnotları"
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(clip), Paste: true})
	clock = clock.Add(50 * time.Millisecond)
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if c.subject.Value() != "Toplantı notları" {
		t.Fatalf("Konu: %q", c.subject.Value())
	}
	// a second paste later is a real second paste
	clock = clock.Add(2 * time.Second)
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if c.subject.Value() != "Toplantı notlarıToplantı notları" {
		t.Fatalf("second paste: %q", c.subject.Value())
	}
	// body: lines kept, CR LF → LF, control characters dropped, tab → spaces
	c.focus = fBody
	c.applyFocus()
	c.body.SetValue("")
	clip = "Bir\r\n\tİki\x07\r\nÜç"
	clock = clock.Add(2 * time.Second)
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if c.body.Value() != "Bir\n    İki\nÜç" {
		t.Fatalf("body: %q", c.body.Value())
	}
	// absurd size: cut, with a note
	clip = strings.Repeat("x", edit.MaxBody+10)
	clock = clock.Add(2 * time.Second)
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if !hasToast(m, "çok uzundu") || len([]rune(c.body.Value())) != edit.MaxBody {
		t.Fatal("huge paste not capped")
	}
}

func TestRightClickRouting(t *testing.T) {
	m, _, c := newCompose(t)
	m.applySize(120, 40)
	clip := "yapıştırılan"
	stubClipboard(t, &clip)
	m.View()
	x, y := find(t, m, "Konu") // the subject field's row
	mouse(m, x+40, y, tea.MouseActionPress, tea.MouseButtonRight)
	if c.focus != fSubject || c.subject.Value() != "bak burayapıştırılan" {
		t.Fatalf("right-click in Konu: focus %d %q", c.focus, c.subject.Value())
	}
	// left-drag selects in a field; typing replaces it
	x, y = find(t, m, "şuna bir bak.")
	mouse(m, x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	mouse(m, x+4, y, tea.MouseActionMotion, tea.MouseButtonLeft)
	mouse(m, x+4, y, tea.MouseActionRelease, tea.MouseButtonLeft)
	if c.focus != fBody || c.body.Selection() != "şuna" {
		t.Fatalf("drag in body: %q", c.body.Selection())
	}
	// list: right-click on the search row pastes into the search
	m2, _ := memModel(t, 3)
	m2.View()
	x, y = find(t, m2, "Ara:")
	mouse(m2, x+8, y, tea.MouseActionPress, tea.MouseButtonRight)
	if m2.search == nil || m2.search.query() != "yapıştırılan" {
		t.Fatal("right-click on the search row")
	}
	// right-click elsewhere (a folder row) changes nothing
	before := m2.folder
	x, y = find(t, m2, "Engellenenler")
	mouse(m2, x, y, tea.MouseActionPress, tea.MouseButtonRight)
	if m2.folder != before || m2.blocks != nil {
		t.Fatal("right-click on a folder row did something")
	}
}

func TestIdleMotionStillSkipsTheFrame(t *testing.T) {
	m, _ := selModel(t)
	m.View()
	m.Update(tea.MouseMsg{X: 50, Y: 10, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if !m.noRender {
		t.Fatal("button-less motion must not build a frame")
	}
}

func TestDragAtEdgeScrolls(t *testing.T) {
	m, log := selModel(t)
	m.applySize(60, 14) // a short window: the mail does not fit
	m.View()
	press(m, "enter")
	m.View()
	r := m.tv.r // (the pane or, on a narrow window, the mail's own tab)
	if !m.tv.ok || len(m.tv.lines) <= m.tv.h {
		t.Skipf("mail fits (%d lines, %d rows)", len(m.tv.lines), m.tv.h)
	}
	x0, y0 := m.tv.x, m.tv.y
	mouse(m, x0, y0, tea.MouseActionPress, tea.MouseButtonLeft)
	cmd := m.onMouse(tea.MouseMsg{X: x0 + 3, Y: m.tv.y + m.tv.h + 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if r.scroll != 1 || cmd == nil {
		t.Fatalf("drag below the text: scroll %d, tick %v", r.scroll, cmd != nil)
	}
	m.Update(dragTickMsg{}) // still held there: keeps scrolling
	if r.scroll != 2 {
		t.Fatalf("tick: scroll %d", r.scroll)
	}
	mouse(m, x0+3, m.tv.y+m.tv.h+2, tea.MouseActionRelease, tea.MouseButtonLeft)
	takeCopy(m)
	if got := copies(log); len(got) != 1 || !strings.HasPrefix(got[0], "Merhaba") {
		t.Fatalf("%q", got)
	}
	m.Update(dragTickMsg{}) // released: the tick stops
	if r.scroll != 2 {
		t.Fatal("scrolled after release")
	}
}
