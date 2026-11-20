package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/edit"
	"termail/internal/htmltext"
	"termail/internal/mailmsg"
)

const linkMail = "From: Banka <info@banka.com.tr>\r\nTo: ben@test.local\r\nSubject: Hesap\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
	`<p>Merhaba, <a href="https://banka.com.tr/kampanya">kampanyayı incele</a> ve ` +
	`<a href="https://evil.example/giris">https://www.banka.com.tr</a>.</p>` +
	`<p>Sıkça sorulanlar: https://banka.com.tr/sss. Tekrar: <a href="https://banka.com.tr/kampanya/">kampanya</a></p>` +
	`<p><a href="javascript:alert(1)">tehlikeli</a> <img src="https://t.example/p.gif" width="1" height="1"></p>`

// linkModel: the test model, its first mail carrying linkMail.
func linkModel(t *testing.T, w, h int) (*Model, *Acct) {
	t.Helper()
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	m, a := testModel(t)
	a.Open = true
	p, err := mailmsg.Parse([]byte(linkMail))
	if err != nil {
		t.Fatal(err)
	}
	m.bodies.Put(bodyKey{0, "INBOX", 3}, p)
	m.applySize(w, h)
	return m, a
}

func stubOpen(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	old, oldCopy := openURLFn, copyTextFn
	openURLFn = func(u string) error { opened = append(opened, u); return nil }
	copyTextFn = func(s string) { opened = append(opened, "copy:"+s) }
	t.Cleanup(func() { openURLFn, copyTextFn = old, oldCopy })
	return &opened
}

func frameLines(m *Model) []string {
	var out []string
	for _, l := range strings.Split(m.View(), "\n") {
		out = append(out, ansi.Strip(l))
	}
	return out
}

// find returns the screen cell of the first occurrence of s.
func find(t *testing.T, m *Model, s string) (int, int) {
	t.Helper()
	for y, l := range frameLines(m) {
		if i := strings.Index(l, s); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", s, strings.Join(frameLines(m), "\n"))
	return 0, 0
}

func click(m *Model, x, y int) {
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.View()
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m.View()
}

func wheel(m *Model, x, y int, down bool) {
	b := tea.MouseButtonWheelUp
	if down {
		b = tea.MouseButtonWheelDown
	}
	m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b})
	m.View()
}

// ---- links ----

func TestLinkMarkersInReader(t *testing.T) {
	for _, sz := range sizes {
		m, _ := linkModel(t, sz[0], sz[1])
		checkFrame(t, fmt.Sprint("pane ", sz), m)
		if e, ok := m.selected(); ok {
			ai, fo := m.loc(e)
			m.openReader(ai, fo, e)
		}
		checkFrame(t, fmt.Sprint("tab ", sz), m)
		if strings.ContainsRune(m.View(), htmltext.MarkRune) {
			t.Fatal("marker character leaked to the terminal")
		}
	}
	m, _ := linkModel(t, 160, 40)
	all := strings.Join(frameLines(m), "\n")
	for _, s := range []string{"kampanyayı incele [1]", "https://www.banka.com.tr [2]", "https://banka.com.tr/sss [3]", "kampanya [1]"} {
		if !strings.Contains(all, s) {
			t.Errorf("reader lacks %q:\n%s", s, all)
		}
	}
	if strings.Contains(all, "javascript") || strings.Contains(all, "t.example") || strings.Contains(all, "tehlikeli [") {
		t.Error("unsafe or tracking link numbered")
	}
}

func TestLinksKeyDoesNotStealL(t *testing.T) {
	m, _ := linkModel(t, 160, 40)
	press(m, "enter") // into the reading pane
	press(m, "l")
	if m.links != nil {
		t.Fatal("l opened the link list; it belongs to attachments (like →)")
	}
	press(m, linksKey)
	if m.links == nil || len(m.links.links) != 3 {
		t.Fatalf("w did not open the list: %+v", m.links)
	}
}

func TestLinkPicker(t *testing.T) {
	opened := stubOpen(t)
	m, _ := linkModel(t, 160, 40)
	press(m, linksKey) // also works from the list for the highlighted mail
	if m.links == nil {
		t.Fatal("picker not open")
	}
	all := strings.Join(frameLines(m), "\n")
	for _, s := range []string{"Bağlantılar", "[1] kampanyayı incele — banka.com.tr", "[2] https://www.banka.com.tr — evil.example · Dikkat: farklı adrese gidiyor", "[3]"} {
		if !strings.Contains(all, s) {
			t.Errorf("picker lacks %q:\n%s", s, all)
		}
	}
	if strings.Count(all, "Dikkat") != 1 {
		t.Error("warning on a matching link")
	}
	press(m, "down")
	press(m, "y")
	if len(*opened) != 1 || (*opened)[0] != "copy:https://evil.example/giris" || !hasToast(m, "Kopyalandı") {
		t.Fatalf("copy: %q", *opened)
	}
	press(m, "enter")
	if m.links != nil || len(*opened) != 2 || (*opened)[1] != "https://evil.example/giris" || !hasToast(m, "Tarayıcıda açılıyor") {
		t.Fatalf("enter: %q", *opened)
	}
	press(m, linksKey)
	press(m, "3") // digit opens at once
	if m.links != nil || (*opened)[2] != "https://banka.com.tr/sss" {
		t.Fatalf("digit: %q", *opened)
	}
	press(m, linksKey)
	press(m, "esc")
	if m.links != nil || len(*opened) != 3 {
		t.Fatal("esc")
	}
	press(m, linksKey)
	press(m, "9")
	if m.links == nil || len(*opened) != 3 {
		t.Fatal("a number with no link opened something / closed the list")
	}
}

func TestLinkPickerManyLinks(t *testing.T) {
	opened := stubOpen(t)
	m, _ := linkModel(t, 160, 40)
	var ls []htmltext.Link
	for i := 1; i <= 15; i++ {
		ls = append(ls, htmltext.Link{URL: fmt.Sprintf("https://x.com/%d", i), Text: fmt.Sprint("bağlantı ", i)})
	}
	m.links = &linkPicker{links: ls}
	press(m, "1") // could be 1 or 1x: waits
	if len(*opened) != 0 || m.links == nil {
		t.Fatal("opened before the number was complete")
	}
	press(m, "2")
	if len(*opened) != 1 || (*opened)[0] != "https://x.com/12" {
		t.Fatalf("12: %q", *opened)
	}
	m.links = &linkPicker{links: ls}
	press(m, "1")
	press(m, "enter")
	if (*opened)[1] != "https://x.com/1" {
		t.Fatalf("1+Enter: %q", *opened)
	}
	m.links = &linkPicker{links: ls}
	press(m, "7")
	if (*opened)[2] != "https://x.com/7" {
		t.Fatalf("7 (no 7x link): %q", *opened)
	}
	m.links = &linkPicker{links: ls}
	for i := 0; i < 20; i++ {
		press(m, "down")
	}
	checkFrame(t, "many", m)
	if m.links.cur != 14 {
		t.Fatal(m.links.cur)
	}
}

func TestNoLinksSaysSo(t *testing.T) {
	m, _ := linkModel(t, 160, 40)
	press(m, "down")
	m.bodies.Put(bodyKey{0, "INBOX", 2}, &mailmsg.Parsed{Text: "düz metin"})
	press(m, linksKey)
	if m.links != nil || !hasToast(m, "bağlantı yok") {
		t.Fatal("no-link mail")
	}
}

func TestBrowserCommandsInjectionSafe(t *testing.T) {
	cmds, err := browserCommands("windows", "https://x.com/a?b=1&c=calc|x%20(y)!")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 || cmds[0][0] != "rundll32" || len(cmds[0]) != 3 || cmds[0][2] != "https://x.com/a?b=1&c=calc|x%20(y)!" {
		t.Fatalf("rundll32: %q", cmds[0])
	}
	if got := cmds[1]; strings.Join(got[:4], " ") != "cmd /c start " || got[4] != "https://x.com/a?b=1^&c=calc^|x^%20^(y^)^!" {
		t.Fatalf("cmd fallback not escaped: %q", got)
	}
	for _, bad := range []string{"javascript:alert(1)", "file:///C:/Windows/System32/calc.exe", `https://x.com/"&calc&"`, "https://x.com/a b",
		"calc.exe", `\\server\share\x.exe`, "data:text/html,x", "ms-settings:", "https://x.com/\r\n"} {
		if c, err := browserCommands("windows", bad); err == nil {
			for _, arg := range c[0] {
				if strings.ContainsAny(arg, "\" \r\n") {
					t.Errorf("%q passed through as %q", bad, c)
				}
			}
			if strings.HasPrefix(bad, "javascript") || strings.HasPrefix(bad, "file") || strings.HasPrefix(bad, "data") || strings.HasPrefix(bad, "ms-") || bad == "calc.exe" || strings.HasPrefix(bad, `\\`) {
				t.Errorf("%q accepted", bad)
			}
		}
	}
}

// ---- mouse ----

func TestMouseClickRowsAtSizes(t *testing.T) {
	for _, sz := range [][2]int{{160, 40}, {140, 40}, {100, 30}, {90, 24}, {70, 24}} {
		m, a := linkModel(t, sz[0], sz[1])
		m.View()
		x, y := find(t, m, "Ayşe")
		click(m, x, y)
		if m.cursor != 1 || m.focus != focusList || m.pv == nil || m.pv.key.uid != 2 {
			t.Fatalf("%v: row click cursor %d focus %d", sz, m.cursor, m.focus)
		}
		if seen(a, 2) {
			t.Fatalf("%v: a single click marked the mail read (keyboard rules)", sz)
		}
		// folder panel: open it on narrow windows first (as ← does)
		if l := m.layout(); l.lw == 0 {
			press(m, "left")
		}
		trash := a.folder("Trash").Label()
		x, y = find(t, m, trash)
		click(m, x, y)
		if m.folder != "Trash" || m.focus != focusFolders {
			t.Fatalf("%v: folder click folder %q focus %d", sz, m.folder, m.focus)
		}
		x, y = find(t, m, "Gelen Kutusu")
		click(m, x+1, y)
		if m.folder != "INBOX" {
			t.Fatalf("%v: back to inbox: %q", sz, m.folder)
		}
		checkFrame(t, fmt.Sprint(sz), m)
	}
}

func TestDoubleClickOpensTab(t *testing.T) {
	now := time.Date(2026, 11, 14, 14, 0, 0, 0, time.Local)
	m, a := linkModel(t, 160, 40)
	m.opt.Now = func() time.Time { return now }
	x, y := find(t, m, "Ayşe")
	click(m, x, y)
	now = now.Add(500 * time.Millisecond)
	click(m, x, y)
	if len(m.tabs) != 1 {
		t.Fatal("two slow clicks opened a tab")
	}
	now = now.Add(300 * time.Millisecond)
	click(m, x, y)
	if len(m.tabs) != 2 || m.tabs[1].read == nil || m.tabs[1].read.key.uid != 2 || !seen(a, 2) {
		t.Fatalf("double click: %d tabs", len(m.tabs))
	}
	// a click on another row right after is not a double click
	m.active = 0
	m.View()
	x2, y2 := find(t, m, "Can")
	now = now.Add(100 * time.Millisecond)
	click(m, x2, y2)
	if len(m.tabs) != 2 {
		t.Fatal("click on a different row counted as double click")
	}
}

func bigList(m *Model, a *Acct, n int) {
	now := time.Now()
	var l []backend.Envelope
	for i := 0; i < n; i++ {
		l = append(l, backend.Envelope{UID: uint32(n - i + 10), From: fmt.Sprint("Kişi ", i), Subject: fmt.Sprint("Konu ", i), Date: now.Add(-time.Duration(i) * time.Minute)})
	}
	a.Lists["INBOX"] = l
	m.cursor = 0
	m.previewCmd()
	m.bodies.Put(bodyKey{0, "INBOX", l[0].UID}, &mailmsg.Parsed{Text: strings.Repeat("satır\n", 200)})
}

func TestWheelRouting(t *testing.T) {
	m, a := linkModel(t, 160, 40)
	bigList(m, a, 100)
	m.View()
	lx, ly := find(t, m, "Kişi 3")
	wheel(m, lx, ly, true)
	if m.offset != 3 || m.cursor != 0 {
		t.Fatalf("wheel over list: offset %d cursor %d", m.offset, m.cursor)
	}
	wheel(m, lx, ly, true)
	wheel(m, lx, ly, false)
	if m.offset != 3 {
		t.Fatal(m.offset)
	}
	// over the reading pane: scrolls the text, not the list — whatever has focus
	m.focus = focusFolders
	px, py := find(t, m, "satır")
	wheel(m, px, py, true)
	if m.pv.scroll != 3 || m.offset != 3 {
		t.Fatalf("wheel over pane: scroll %d offset %d", m.pv.scroll, m.offset)
	}
	// a key brings the cursor row back into view (keyboard unchanged)
	m.focus = focusList
	press(m, "down")
	m.View()
	if m.cursor != 1 || m.offset != 1 {
		t.Fatalf("after key: cursor %d offset %d", m.cursor, m.offset)
	}
	for i := 0; i < 50; i++ {
		wheel(m, lx, ly, true)
	}
	checkFrame(t, "wheel end", m)
	if m.offset > 100 {
		t.Fatal("scrolled past the end")
	}
}

func TestNoRenderOnMotion(t *testing.T) {
	m, _ := linkModel(t, 160, 40)
	before := m.View()
	frames := m.frames
	x, y := 0, 0
	found := false
	for yy := 0; yy < m.h && !found; yy++ {
		for xx := 0; xx < m.w; xx++ {
			if _, ok := m.hits.at(xx, yy); !ok {
				x, y, found = xx, yy, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no non-clickable cell")
	}
	for i := 0; i < 20; i++ {
		m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
		if v := m.View(); v != before {
			t.Fatal("motion over a non-clickable cell changed the frame")
		}
	}
	if m.frames != frames {
		t.Fatalf("motion built %d new frames", m.frames-frames)
	}
	// a real event after motion renders again
	press(m, "down")
	if m.View() == before {
		t.Fatal("frame not rebuilt after a key")
	}
}

func TestHoverHighlightsClickable(t *testing.T) {
	m, _ := testModel(t)
	m.View()
	var row hit
	for _, x := range m.hits.list {
		if x.kind == hitListRow && x.idx == 0 {
			row = x
		}
	}
	if row.w == 0 {
		t.Fatal("list row hit missing")
	}
	m.Update(tea.MouseMsg{X: row.x + 2, Y: row.y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if m.noRender || !m.hoverOn || m.hover.kind != hitListRow {
		t.Fatalf("hover not set: on=%v kind=%v noRender=%v", m.hoverOn, m.hover.kind, m.noRender)
	}
	m.View()
	frames := m.frames
	m.Update(tea.MouseMsg{X: row.x + 2, Y: row.y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	m.View()
	if m.frames != frames {
		t.Fatal("motion over the same target should not rebuild")
	}
	cx, cy, ok := 0, 0, false
	for yy := 0; yy < m.h && !ok; yy++ {
		for xx := 0; xx < m.w; xx++ {
			if _, hit := m.hits.at(xx, yy); !hit {
				cx, cy, ok = xx, yy, true
				break
			}
		}
	}
	if !ok {
		t.Fatal("no non-clickable cell")
	}
	m.Update(tea.MouseMsg{X: cx, Y: cy, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if m.hoverOn {
		t.Fatal("hover should clear when the pointer leaves a clickable")
	}
}

func TestTabClicks(t *testing.T) {
	m, _ := linkModel(t, 160, 40)
	if e, ok := m.selected(); ok {
		ai, fo := m.loc(e)
		m.openReader(ai, fo, e)
	}
	if len(m.tabs) != 2 || m.active != 1 {
		t.Fatal("no tab")
	}
	m.View()
	x, y := find(t, m, "1 ≡")
	click(m, x, y)
	if m.active != 0 {
		t.Fatal("tab click did not switch")
	}
	x, y = find(t, m, "×")
	click(m, x, y)
	if len(m.tabs) != 1 {
		t.Fatal("× did not close the tab")
	}
	// compose tab with text: × keeps it as a draft exactly like Ctrl+W
	m.openCompose(composeNew, nil, backend.Envelope{})
	c := m.tabs[m.active].compose
	c.body.SetValue("yarım kalan metin")
	m.View()
	x, y = find(t, m, "×")
	click(m, x, y)
	if m.confirm == nil || !strings.Contains(m.confirm.text, "Taslak olarak kaydedilsin mi?") {
		t.Fatal("× on an unsent compose should ask whether to save a draft")
	}
}

func TestClickLinkInReader(t *testing.T) {
	opened := stubOpen(t)
	m, _ := linkModel(t, 160, 40)
	x, y := find(t, m, "[3]")
	click(m, x+1, y)
	if len(*opened) != 1 || (*opened)[0] != "https://banka.com.tr/sss" || !hasToast(m, "Tarayıcıda açılıyor") {
		t.Fatalf("link click: %q", *opened)
	}
	if e, ok := m.selected(); ok {
		ai, fo := m.loc(e)
		m.openReader(ai, fo, e)
	}
	x, y = find(t, m, "incele [1]")
	click(m, x, y) // the link text right before the number works too
	if len(*opened) != 2 || (*opened)[1] != "https://banka.com.tr/kampanya" {
		t.Fatalf("tab link click: %q", *opened)
	}
}

func TestClickPickerRowAndAccountRow(t *testing.T) {
	opened := stubOpen(t)
	m, _ := linkModel(t, 160, 40)
	press(m, linksKey)
	m.View()
	x, y := find(t, m, "[3] ")
	click(m, x+5, y)
	if m.links != nil || len(*opened) != 1 || (*opened)[0] != "https://banka.com.tr/sss" {
		t.Fatalf("picker row click: %q", *opened)
	}
	// clicks outside an overlay do nothing
	press(m, linksKey)
	m.View()
	_, ly := find(t, m, "Bağlantılar")
	click(m, 1, ly+30)
	if m.links == nil {
		t.Fatal("click outside closed the picker / passed through")
	}
	press(m, "esc")
	m.acctPicker = newAccountPicker(m)
	m.View()
	_, ty := find(t, m, "Hesaba geç")
	lines := frameLines(m)
	for y = ty + 1; y < len(lines) && !strings.Contains(lines[y], " 0 "); y++ {
	}
	x = ansi.StringWidth(lines[y][:strings.Index(lines[y], " 0 ")]) + 1
	click(m, x, y)
	if m.acctPicker != nil || m.virt != viewUnified {
		t.Fatal("account picker row click")
	}
}

func TestClickToastClose(t *testing.T) {
	m, _ := linkModel(t, 160, 40)
	m.toastInfo(0, "", "Merhaba bildirimi")
	m.View()
	_, y := find(t, m, "Merhaba bildirimi")
	lines := frameLines(m)
	xi := strings.LastIndex(lines[y-1], "×")
	if xi < 0 {
		t.Fatalf("no × on toast:\n%s", strings.Join(lines, "\n"))
	}
	click(m, ansi.StringWidth(lines[y-1][:xi]), y-1)
	if len(m.toasts.Actions()) != 0 {
		t.Fatal("toast × did not close it")
	}
}

func TestClickComposeFields(t *testing.T) {
	m, _, c := newCompose(t)
	m.applySize(120, 40)
	m.View()
	x, y := find(t, m, "Konu")
	click(m, x, y)
	if c.focus != fSubject {
		t.Fatalf("focus %d", c.focus)
	}
	x, y = find(t, m, "şuna bir bak.")
	click(m, x+3, y)
	if c.focus != fBody || c.body.Cursor() != (edit.Pos{Line: 1, Col: 3}) {
		t.Fatalf("body click: focus %d cursor %+v", c.focus, c.body.Cursor())
	}
	x, y = find(t, m, "Ekler")
	click(m, x, y)
	if c.focus != fAttach {
		t.Fatal("Ekler")
	}
	x, y = find(t, m, "Kime")
	click(m, x, y)
	if c.focus != fTo {
		t.Fatal("Kime")
	}
}

// The keyboard must behave exactly as before: the same key sequence gives
// the same frames whether or not the mouse is on.
func TestKeyboardUnchangedByMouseSupport(t *testing.T) {
	seq := []string{"down", "down", "enter", "down", "esc", "left", "down", "enter", "right", "o", "tab", linksKey, "down", "esc", "up", "pgdown", "?", "esc"}
	run := func(mouse bool) []string {
		m, _ := linkModel(t, 140, 36)
		m.mouse = mouse
		var frames []string
		for _, k := range seq {
			press(m, k)
			frames = append(frames, m.View())
		}
		return frames
	}
	a, b := run(false), run(true)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("frame after %q differs with the mouse on", seq[i])
		}
	}
	// and pointer motion between keys changes nothing either
	m, _ := linkModel(t, 140, 36)
	for i, k := range seq {
		m.Update(tea.MouseMsg{X: 3 * i, Y: i, Action: tea.MouseActionMotion})
		press(m, k)
		if v := m.View(); v != a[i] {
			t.Fatalf("frame after %q differs when the pointer moved before it", k)
		}
	}
}

func TestMouseConfig(t *testing.T) {
	for _, v := range []string{"kapalı", "Kapalı", " kapali ", "off", "hayır", "false"} {
		if (&account.Config{Mouse: v}).MouseOn() {
			t.Errorf("fare: %q left the mouse on", v)
		}
	}
	for _, v := range []string{"", "açık", "acik", "on"} {
		if !(&account.Config{Mouse: v}).MouseOn() {
			t.Errorf("fare: %q turned the mouse off", v)
		}
	}
	var c account.Config
	if err := json.Unmarshal([]byte(`{"accounts":[],"fare":"kapalı"}`), &c); err != nil || c.MouseOn() {
		t.Fatalf("config file setting not read: %v %+v", err, c)
	}
	var nilCfg *account.Config
	if !nilCfg.MouseOn() {
		t.Fatal("no config: mouse should be on")
	}
}
