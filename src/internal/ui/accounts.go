package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/grid"
)

// ---- unified inbox ("Tüm Gelen Kutuları") ----

const unifiedTitle = "Tüm Gelen Kutuları"

// acctColors label accounts in the unified inbox (cycled).
var acctColors = []string{cAccent, cGreen, cYellow, cPurple, cRed}

func acctColor(i int) string { return acctColors[i%len(acctColors)] }

// shortName is the account label in the unified list ("Kişisel" -> "Kişisel", "Muhasebe" -> "Muhase").
func shortName(name string) string { return grid.Truncate(name, 7) }

// mergeInboxes returns every account's inbox as one flat list, newest first.
// Each envelope's Acct field tells which account it belongs to.
func mergeInboxes(accts []*Acct) []backend.Envelope {
	var out []backend.Envelope
	for i, a := range accts {
		for _, e := range a.Lists[a.inbox()] {
			e.Acct, e.Folder = i, a.inbox()
			out = append(out, e)
		}
	}
	sortMerged(out)
	return out
}

// mergeSaved returns the saved (flagged) mail of all accounts, newest first,
// with duplicates (the same mail found in two folders) removed.
func mergeSaved(accts []*Acct) []backend.Envelope {
	var out []backend.Envelope
	for i, a := range accts {
		seen := map[string]bool{}
		for _, e := range a.Saved {
			if e.MessageID != "" {
				if seen[e.MessageID] {
					continue
				}
				seen[e.MessageID] = true
			}
			e.Acct = i
			out = append(out, e)
		}
	}
	sortMerged(out)
	return out
}

func sortMerged(out []backend.Envelope) {
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}
		if out[i].Acct != out[j].Acct {
			return out[i].Acct < out[j].Acct
		}
		return out[i].UID > out[j].UID
	})
}

type virtView int

const (
	viewFolder  virtView = iota
	viewUnified          // "Tüm Gelen Kutuları"
	viewSaved            // "Kaydedilenler"
)

// loc tells which account and folder a listed mail lives in.
func (m *Model) loc(e backend.Envelope) (int, string) {
	// merged views and search results carry their own account and folder
	if (m.virt != viewFolder || m.searching()) && e.Acct < len(m.accts) && e.Folder != "" {
		return e.Acct, e.Folder
	}
	return m.cur, m.folder
}

// showing reports whether the list on screen contains the given folder.
func (m *Model) showing(ai int, folder string) bool {
	switch m.virt {
	case viewUnified:
		return ai < len(m.accts) && folder == m.accts[ai].inbox()
	case viewSaved:
		return false
	}
	return ai == m.cur && folder == m.folder
}

// inboxUnread is the unread count of an account's inbox.
func (a *Acct) inboxUnread() int {
	if f := a.folder(a.inbox()); f != nil {
		return f.Unread
	}
	return 0
}

func (m *Model) totalUnread() int {
	n := 0
	for _, a := range m.accts {
		n += a.inboxUnread()
	}
	return n
}

// switchAccount shows account i's inbox at once.
func (m *Model) switchAccount(i int) tea.Cmd {
	if i < 0 || i >= len(m.accts) {
		return nil
	}
	a := m.accts[i]
	m.virt = viewFolder
	m.setOpen(i, true)
	m.active = 0
	m.acctPicker = nil
	cmd := m.openFolder(i, a.inbox())
	m.syncFolderCursor()
	return cmd
}

// showUnified switches to "Tüm Gelen Kutuları".
func (m *Model) showUnified() tea.Cmd { return m.showVirtual(viewUnified) }

// showSaved switches to "Kaydedilenler" and refreshes it from the servers.
func (m *Model) showSaved() tea.Cmd { return m.showVirtual(viewSaved) }

func (m *Model) showVirtual(v virtView) tea.Cmd {
	return m.showVirtualView(v, false)
}

// showVirtualView shows a merged view; read picks Okunanlar of the
// unified inboxes.
func (m *Model) showVirtualView(v virtView, read bool) tea.Cmd {
	if len(m.accts) == 0 {
		return nil
	}
	m.virt = v
	m.readView = read && v == viewUnified
	m.stickySec, m.opened = nil, nil
	m.active = 0
	m.acctPicker = nil
	m.cursor, m.offset = 0, 0
	m.dropSearch()
	m.focus = focusList
	m.syncFolderCursor()
	var cmds []tea.Cmd
	for i, a := range m.accts {
		if v == viewUnified && !a.Loaded[a.inbox()] && a.B != nil {
			cmds = append(cmds, m.loadList(i, a.inbox(), false))
		}
		if v == viewSaved {
			cmds = append(cmds, m.loadSaved(i))
		}
	}
	return tea.Batch(append(cmds, m.previewCmd())...)
}

// ---- account keys ----

// accountKey maps F1–F12 to an account switch: F1 = Tüm Gelen Kutuları
// (index -1), F2..F12 = account 1..11. Digit keys never switch accounts
// (they are reserved for typing, links, and attachments).
func accountKey(key string) (int, bool) {
	if strings.HasPrefix(key, "f") {
		if n, err := strconv.Atoi(key[1:]); err == nil && n >= 1 && n <= 12 {
			return n - 2, true // f1 -> -1
		}
	}
	return 0, false
}

// ctrlDigitFromCSI recognises Ctrl+digit in the escape sequences that
// Bubble Tea passes through as unknown CSI messages. The argument is the
// message's String() form, e.g. "?CSI[52 57 59 53 117]?" for "\x1b[49;5u"
// (kitty) or the bytes of "\x1b[27;5;49~" (xterm modifyOtherKeys).
func ctrlDigitFromCSI(s string) (string, bool) {
	if !strings.HasPrefix(s, "?CSI[") || !strings.HasSuffix(s, "]?") {
		return "", false
	}
	var b []byte
	for _, f := range strings.Fields(s[5 : len(s)-2]) {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > 255 {
			return "", false
		}
		b = append(b, byte(n))
	}
	seq := string(b)
	var code, mod int
	switch {
	case strings.HasSuffix(seq, "u"): // kitty: <code>;<mod>u
		if _, err := fmt.Sscanf(seq, "%d;%du", &code, &mod); err != nil {
			return "", false
		}
	case strings.HasPrefix(seq, "27;") && strings.HasSuffix(seq, "~"): // xterm: 27;<mod>;<code>~
		if _, err := fmt.Sscanf(seq, "27;%d;%d~", &mod, &code); err != nil {
			return "", false
		}
	default:
		return "", false
	}
	if code < '0' || code > '9' || (mod-1)&4 == 0 { // modifier bit 4 = Ctrl
		return "", false
	}
	return "ctrl+" + string(rune(code)), true
}

// ---- account picker (Ctrl+A) ----

type accountPicker struct{ cur int } // 0 = unified, 1.. = accounts, last = add

func newAccountPicker(m *Model) *accountPicker {
	p := &accountPicker{cur: m.cur + 1}
	if m.virt == viewUnified {
		p.cur = 0
	}
	return p
}

func (p *accountPicker) key(m *Model, key string) tea.Cmd {
	n := len(m.accts) + 2
	switch key {
	case "esc", "ctrl+a", "q":
		m.acctPicker = nil
	case "up":
		p.cur = (p.cur - 1 + n) % n
	case "down", "tab":
		p.cur = (p.cur + 1) % n
	case "enter":
		return p.choose(m, p.cur)
	case "+":
		return p.choose(m, n-1)
	default:
		if i, ok := accountKey(key); ok {
			return p.choose(m, i+1)
		}
	}
	return nil
}

func (p *accountPicker) choose(m *Model, row int) tea.Cmd {
	m.acctPicker = nil
	switch {
	case row == 0:
		return m.showUnified()
	case row >= 1 && row <= len(m.accts):
		return m.switchAccount(row - 1)
	case row == len(m.accts)+1:
		m.login = newLogin(m, true)
		m.login.resize(m.w)
	}
	return nil
}

func (p *accountPicker) draw(m *Model, g *grid.Grid) {
	w := min(64, g.W-4)
	h := len(m.accts) + 8
	x, y := (g.W-w)/2, max(1, (g.H-h)/2-2)
	g.Box(x, y, w, h, cBorderF, cPanel, "Hesaba geç", cAccent)
	g.RPut(x+w-3, y, " Esc: kapat ", st(cDim))
	m.hits.add(hit{w: g.W, h: g.H, kind: hitModal})
	row := func(i, yy int, num, name, sub string, unread int, color string, active bool) {
		m.hits.add(hit{x: x + 1, y: yy, w: w - 2, h: 1, kind: hitAcctRow, idx: i})
		bg := ""
		if i == p.cur {
			g.Fill(x+1, yy, w-2, 1, cSelFocus)
			bg = cSelFocus
		}
		if num != "" {
			g.Put(x+3, yy, " "+num+" ", stbg(cFG, cKey), 0)
		}
		mark := "  "
		if active {
			mark = "▌ "
		}
		g.Put(x+7, yy, mark, grid.Style{FG: cAccent, BG: bg}, 0)
		nx := g.Put(x+9, yy, "■ ", grid.Style{FG: color, BG: bg}, 0)
		nx = g.Put(nx, yy, name, grid.Style{FG: cFG, BG: bg, Bold: true}, 18)
		g.Put(x+31, yy, sub, grid.Style{FG: cDim, BG: bg}, w-31-8)
		if unread > 0 {
			g.RPut(x+w-3, yy, strconv.Itoa(unread), grid.Style{FG: cYellow, BG: bg, Bold: true})
		}
	}
	yy := y + 2
	row(0, yy, "0", unifiedTitle, "", m.totalUnread(), cFG, m.virt == viewUnified)
	yy += 2
	for i, a := range m.accts {
		num := ""
		if i < 9 {
			num = strconv.Itoa(i + 1)
		}
		sub := a.Cfg.Email
		switch a.State {
		case stNeedsLogin:
			sub = "giriş gerekli"
		case stOffline:
			sub = "çevrimdışı"
		}
		row(i+1, yy, num, a.Cfg.Name, sub, a.inboxUnread(), acctColor(i), m.virt == viewFolder && i == m.cur)
		yy++
	}
	yy++
	add := len(m.accts) + 1
	m.hits.add(hit{x: x + 1, y: yy, w: w - 2, h: 1, kind: hitAcctRow, idx: add})
	bg := ""
	if p.cur == add {
		g.Fill(x+1, yy, w-2, 1, cSelFocus)
		bg = cSelFocus
	}
	g.Put(x+3, yy, " + ", stbg(cFG, cKey), 0)
	g.Put(x+9, yy, "Hesap ekle", grid.Style{FG: cAccent, BG: bg}, 0)
}

// drawAccountChips draws "F1 Tüm Gelen Kutuları  F2 Kişisel 3  F3 İş …"
// into the top bar from x to maxX. The active account (or F1) is a solid
// filled chip; the others stay dim. Each chip is clickable.
func (m *Model) drawAccountChips(g *grid.Grid, x, maxX int) {
	put := func(idx int, fkey, label string, n int, active bool) bool {
		w := 1 + grid.Width(fkey) + 1 + grid.Width(label) + 1
		if n > 0 {
			w += 1 + len(strconv.Itoa(n))
		}
		if x+w+1 > maxX {
			return false
		}
		fg, bg := cDim, cBar
		keyFG := cFaint
		if active {
			fg, bg, keyFG = cChipFG, cChip, cChipFG
			g.Fill(x, 0, w, 1, bg)
		}
		m.hits.add(hit{x: x, y: 0, w: w, h: 1, kind: hitAcctChip, idx: idx})
		x = g.Put(x, 0, " "+fkey+" ", stbgb(keyFG, bg, active), 0)
		x = g.Put(x, 0, label, grid.Style{FG: fg, BG: bg, Bold: active}, 0)
		if n > 0 {
			x = g.Put(x, 0, " "+strconv.Itoa(n), grid.Style{FG: cYellow, BG: bg, Bold: true}, 0)
		}
		x = g.Put(x, 0, " ", stbg(fg, bg), 0)
		x = g.Put(x, 0, " ", stbg(cDim, cBar), 0)
		return true
	}
	if !put(-1, "F1", unifiedTitle, m.totalUnread(), m.virt == viewUnified) {
		return
	}
	for i, a := range m.accts {
		if i >= 11 { // F2..F12
			break
		}
		if !put(i, fmt.Sprintf("F%d", i+2), a.Cfg.Name, a.inboxUnread(), m.virt == viewFolder && i == m.cur) {
			return
		}
	}
}
