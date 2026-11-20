package ui

import (
	"fmt"
	"strconv"
	"strings"

	"termail/internal/backend"
	"termail/internal/grid"
	"termail/internal/i18n"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

type rowKind int

const (
	rowAcct rowKind = iota
	rowKindLine
	rowFolder
	rowHeading
	rowSep
	rowBlank
	rowBlocked
	rowAddAcct
	rowUnified   // "Tüm Gelen Kutuları"
	rowSaved     // "Kaydedilenler"
	rowNewFolder // inline "new folder" name field
)

type frow struct {
	read   bool // the Okunanlar row (under an inbox or Tüm Gelen Kutuları)
	kind   rowKind
	acct   int
	folder string
	label  string
	count  int
	custom bool
}

func (m *Model) folderRows() []frow {
	var rows []frow
	if len(m.accts) > 0 {
		rows = append(rows, frow{kind: rowUnified, label: "≡ " + unifiedTitle(), count: m.totalUnread()},
			frow{kind: rowSaved, label: "★ " + savedTitle(), count: savedCount(m.accts)})
	}
	for i, a := range m.accts {
		unread := 0
		for _, f := range a.Folders {
			if f.Role == backend.RoleInbox || f.Role == backend.RoleNone {
				unread += f.Unread // inbox + own folders, like the preview
			}
		}
		label := a.Cfg.Email // the tree shows each account by its address
		if label == "" {
			label = a.Cfg.Name
		}
		rows = append(rows, frow{kind: rowAcct, acct: i, label: label, count: unread})
		if !a.Open {
			continue
		}
		if a.State == stNeedsLogin {
			rows = append(rows, frow{kind: rowKindLine, acct: i, label: i18n.T("giriş gerekli (+ tuşu)")})
		} else if a.State == stOffline {
			rows = append(rows, frow{kind: rowKindLine, acct: i, label: i18n.T("çevrimdışı")})
		}
		for _, r := range backend.RoleOrder {
			if r == backend.RoleFlagged || r == backend.RoleArchive || r == backend.RoleAll {
				continue // Kaydedilenler / internal archive targets replace these
			}
			for _, f := range a.Folders {
				if f.Role == r && !backend.NonMailFolder(f.Name) {
					rows = append(rows, frow{kind: rowFolder, acct: i, folder: f.Name, label: f.Label(), count: countFor(f)})
				}
			}
		}
		for _, f := range a.Folders {
			if f.Role == backend.RoleNone && !backend.NonMailFolder(f.Name) {
				rows = append(rows, frow{kind: rowFolder, acct: i, folder: f.Name, label: f.Label(), count: countFor(f), custom: true})
			}
		}
		if m.newFolder != nil && m.newFolder.acct == i && a.Open {
			rows = append(rows, frow{kind: rowNewFolder, acct: i})
		}
		if len(a.Folders) == 0 && a.State == stConnecting && (m.newFolder == nil || m.newFolder.acct != i) {
			rows = append(rows, frow{kind: rowHeading, acct: i, label: i18n.T("Yükleniyor…")})
		}
	}
	return rows
}

func countFor(f backend.Folder) int {
	switch f.Role {
	case backend.RoleDrafts, backend.RoleTrash:
		return f.Total
	case backend.RoleSent, backend.RoleJunk, backend.RoleAll, backend.RoleArchive:
		return 0
	}
	return f.Unread
}

type layout struct {
	lw, mw, rw  int // left, middle, right widths (rw 0 = no reader column)
	top, ph     int
	readerBelow bool
}

func (m *Model) layout() layout {
	l := layout{top: 2, ph: m.h - 3}
	switch {
	case m.w >= 110:
		l.lw = 28
		l.mw = (m.w - l.lw) / 2
		l.rw = m.w - l.lw - l.mw
	case m.w >= 80:
		l.lw = 26
		l.mw = m.w - l.lw
		l.readerBelow = true
	default:
		l.lw = 0
		if m.focus == focusFolders {
			l.lw = min(28, m.w/2)
		}
		l.mw = m.w - l.lw
	}
	return l
}

// View renders the whole screen.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.noRender && m.lastOut != "" {
		// pure pointer motion etc.: same frame, so nothing is redrawn
		m.noRender = false
		return m.lastOut
	}
	m.noRender = false
	m.hits.reset()
	m.tv.ok = false
	m.inView = true // visible() may memoize while drawing (read-only phase)
	defer func() { m.inView, m.visMemo = false, nil }()
	g := grid.Reuse(m.gridBuf, m.w, m.h, cFG, cBG)
	m.gridBuf = g
	if m.login != nil {
		m.login.draw(g)
		return m.finish(g)
	}
	t := m.tabs[m.active]
	m.drawHeader(g, t)
	m.drawTabs(g)
	switch t.kind {
	case tabList:
		m.drawMain(g)
	case tabRead:
		t.read.draw(m, g)
	case tabCompose:
		t.compose.draw(m, g)
	}
	if t.kind != tabCompose {
		m.drawToasts(g)
	}
	modal := func() { m.hits.add(hit{w: g.W, h: g.H, kind: hitModal}) }
	switch {
	case m.help:
		modal()
		m.drawHelp(g)
	case m.blocks != nil:
		modal()
		m.blocks.draw(m, g)
	case m.confirm != nil:
		modal()
		m.confirm.draw(m, g)
	case m.picker != nil:
		modal()
		m.picker.draw(m, g)
	case m.links != nil:
		m.links.draw(m, g)
	case m.acctPicker != nil:
		modal()
		m.acctPicker.draw(m, g)
	}
	if m.ctxMenu != nil {
		m.ctxMenu.draw(m, g)
	}
	m.resolveHover()
	m.paintHover(g)
	return m.finish(g)
}

// finish renders the grid and clamps the frame to the window.
func (m *Model) finish(g *grid.Grid) string {
	w, h := m.w, m.h
	if m.realW > 0 {
		w, h = min(w, m.realW-edgeMargin), min(h, m.realH)
	}
	m.frames++
	out := g.Render(m.profile)
	if !(g.W <= w && g.H == h) {
		out = clampFrame(out, w, h)
	}
	// (When the grid already is exactly H rows of W cells the frame fits;
	// Bubble Tea's line diff then only rewrites rows that changed.)
	m.lastOut = out
	m.markOnce("frame", "ilk kare çizildi")
	return out
}

func accountStatusLabel(a *Acct) (string, string) {
	if a == nil {
		return i18n.T("Çevrimiçi"), cGreen
	}
	switch a.State {
	case stOffline, stNeedsLogin:
		return i18n.T("Çevrimdışı"), cYellow
	case stConnecting:
		return i18n.T("Bağlanıyor"), cAccent
	}
	return i18n.T("Çevrimiçi"), cGreen
}

func acctDisplayName(a *Acct) string {
	if a == nil {
		return ""
	}
	if strings.TrimSpace(a.Cfg.Name) != "" {
		return a.Cfg.Name
	}
	return a.Cfg.Email
}

// statusAcctIndex is the account of the mail or folder currently on
// screen, or -1 when the view is a mixed list with no single account.
func (m *Model) statusAcctIndex() int {
	if len(m.accts) == 0 {
		return -1
	}
	if t := m.tabs[m.active]; t != nil {
		switch t.kind {
		case tabCompose:
			if t.compose != nil && t.compose.from >= 0 && t.compose.from < len(m.accts) {
				return t.compose.from
			}
		case tabRead:
			if t.read != nil && t.read.key.acct >= 0 && t.read.key.acct < len(m.accts) {
				return t.read.key.acct
			}
		}
	}
	if m.focus == focusReader && m.pv != nil && m.pv.key.acct >= 0 && m.pv.key.acct < len(m.accts) {
		return m.pv.key.acct
	}
	if m.focus == focusFolders {
		if r, ok := m.folderCursorRow(); ok {
			if r.kind == rowFolder || r.kind == rowAcct {
				return r.acct
			}
		}
	}
	if m.virt == viewFolder {
		return m.cur
	}
	if e, ok := m.selected(); ok && e.UID != 0 && !isGroupRow(e) {
		ai, _ := m.loc(e)
		if ai >= 0 && ai < len(m.accts) {
			return ai
		}
	}
	return -1
}

func (m *Model) statusText() (string, string) {
	if i := m.statusAcctIndex(); i >= 0 && i < len(m.accts) {
		a := m.accts[i]
		label, col := accountStatusLabel(a)
		// In a mixed view the current mail's account is named when it is offline,
		// so one mailbox going down is not painted as if every account were down.
		if (m.virt == viewUnified || m.virt == viewSaved) && m.tabs[m.active].kind == tabList &&
			(a.State == stOffline || a.State == stNeedsLogin) {
			return acctDisplayName(a) + i18n.T(" çevrimdışı"), col
		}
		return label, col
	}
	var offline []*Acct
	connecting := false
	for _, a := range m.accts {
		switch a.State {
		case stConnecting:
			connecting = true
		case stOffline, stNeedsLogin:
			offline = append(offline, a)
		}
	}
	switch {
	case len(offline) == 1:
		return acctDisplayName(offline[0]) + i18n.T(" çevrimdışı"), cYellow
	case len(offline) > 1:
		return i18n.Tf("%d çevrimdışı", len(offline)), cYellow
	case connecting:
		return i18n.T("Bağlanıyor"), cAccent
	}
	return i18n.T("Çevrimiçi"), cGreen
}

func (m *Model) drawHeader(g *grid.Grid, t *tab) {
	g.Fill(0, 0, g.W, 1, cBar)
	x := g.Put(1, 0, " ◆ Termail ", stbgb(cBG, cAccent, true), 0)
	ctx := ""
	switch t.kind {
	case tabCompose:
		ctx = t.compose.headerText()
	case tabRead:
		ctx = i18n.T("İleti okunuyor")
	default:
		switch {
		case m.virt == viewUnified && m.readView:
			ctx = readTitle() + i18n.T(" · tüm hesaplar")
		case m.virt == viewUnified:
			ctx = unifiedTitle()
		case m.virt == viewSaved:
			ctx = savedTitle()
		default:
			if a := m.acct(); a != nil {
				ctx = a.Cfg.Email // the account name is on the chip / panel title
				if len(m.accts) == 1 {
					ctx = a.Cfg.Name + " · " + a.Cfg.Email
				}
			}
		}
	}
	status, col := m.statusText()
	clock := m.opt.Now().Format("15:04")
	right := status + "   " + clock + " "
	cx := g.Put(x+2, 0, ctx, stbg(cDim, cBar), g.W-x-4-grid.Width(right))
	if len(m.accts) > 1 && t.kind == tabList {
		m.drawAccountChips(g, cx+4, g.W-2-grid.Width(right))
	}
	rx := g.RPut(g.W-1, 0, right, stbg(cDim, cBar))
	_ = rx
	g.Put(g.W-1-grid.Width(right)+grid.Width(status)+1, 0, "●", stbg(col, cBar), 0)
}

func (m *Model) tabTitle(i int) (icon, title, flag string) {
	t := m.tabs[i]
	switch t.kind {
	case tabList:
		name := i18n.T("Gelen Kutusu")
		if m.virt == viewUnified && m.readView {
			return "≡", readTitle(), ""
		} else if m.virt == viewUnified {
			return "≡", unifiedTitle(), ""
		} else if m.virt == viewSaved {
			return "★", savedTitle(), ""
		}
		if a := m.acct(); a != nil {
			if f := a.folder(m.folder); f != nil {
				name = strings.TrimSpace(f.Label())
			}
			if m.readView {
				name = readTitle()
			}
		}
		if m.searching() {
			name = "Arama: " + m.search.query()
		}
		return "≡", name, ""
	case tabRead:
		f := ""
		if t.read.unread {
			f = "unread"
		}
		return "", t.title, f
	default:
		return "", t.title, "draft"
	}
}

func (m *Model) drawTabs(g *grid.Grid) {
	y := 1
	g.Fill(0, y, g.W, 1, cBar)
	right := ""
	if len(m.tabs) > 1 {
		right = "Alt+1-9 · Ctrl+W kapat "
	}
	avail := g.W - grid.Width(right) - 2
	tw := 25
	if len(m.tabs) > 0 && avail/len(m.tabs) < tw+1 {
		tw = max(12, avail/len(m.tabs)-1)
	}
	per := max(1, avail/(tw+1))
	start := 0
	if m.active >= per {
		start = m.active - per + 1
	}
	x := 1
	for i := start; i < len(m.tabs) && i < start+per; i++ {
		a := i == m.active
		// active tab: inverted (accent background, dark bold text);
		// inactive tabs: muted text straight on the bar
		bg, fg, numc := cBar, cFaint, cFaint
		if a {
			bg, fg, numc = cAccent, cBG, cBG
		}
		g.Fill(x, y, tw, 1, bg)
		g.Put(x+1, y, fmt.Sprint(i+1), stbgb(numc, bg, a), 0)
		tx := x + 3
		icon, title, flag := m.tabTitle(i)
		if icon != "" {
			g.Put(tx, y, icon, stbg(fg, bg), 0)
			tx += 2
		}
		room := tw - (tx - x) - 1
		fx := x + tw - 2 // flag column
		if i > 0 {
			room -= 2 // the small × that closes the tab
			fx -= 2
		}
		if flag != "" {
			room -= 2
		}
		g.Put(tx, y, title, stbgb(fg, bg, a), room)
		switch flag {
		case "unread":
			g.Put(fx, y, "●", stbg(map[bool]string{true: cBG, false: cYellow}[a], bg), 0)
		case "draft":
			g.Put(fx, y, "◇", stbg(map[bool]string{true: cBG, false: cPurple}[a], bg), 0)
		}
		m.hits.add(hit{x: x, y: y, w: tw, h: 1, kind: hitTab, idx: i})
		if i > 0 {
			xc := map[bool]string{true: cBG, false: cFaint}[a]
			g.Put(x+tw-2, y, "×", stbg(xc, bg), 0)
			m.hits.add(hit{x: x + tw - 3, y: y, w: 3, h: 1, kind: hitTabClose, idx: i})
		}
		x += tw + 1
	}
	if right != "" {
		g.RPut(g.W-1, y, right, stbg(cFaint, cBar))
	}
}

func (m *Model) drawMain(g *grid.Grid) {
	l := m.layout()
	if l.lw > 0 {
		m.drawFolders(g, 0, l.top, l.lw, l.ph)
	}
	listH := l.ph
	if l.readerBelow {
		listH = l.ph / 2
		if m.focus == focusReader { // reading: give the text most of the room
			listH = max(6, l.ph/3)
		}
	}
	m.drawList(g, l.lw, l.top, l.mw, listH)
	if l.rw > 0 {
		m.drawPreview(g, l.lw+l.mw, l.top, l.rw, l.ph)
	} else if l.readerBelow {
		m.drawPreview(g, l.lw, l.top+listH, l.mw, l.ph-listH)
	}
	m.drawMainKeybar(g)
}

func (m *Model) drawMainKeybar(g *grid.Grid) {
	if m.focus == focusFolders {
		items := []keyHint{{"Enter", i18n.T("Aç")}, {"n", i18n.T("Yeni")}, {"→", "Liste"}, {"←", i18n.T("Geri")}, {"Alt+N", i18n.T("Klasör")}, {"+", i18n.T("Hesap ekle")}}
		if r, ok := m.customFolderAtCursor(); ok {
			items = append(items, keyHint{"d", i18n.T("Sil")}, keyHint{"r", i18n.T("Yeniden adlandır")})
			_ = r
		}
		if r, ok := m.folderCursorRow(); ok {
			switch r.kind {
			case rowUnified:
				if !r.read {
					items = append(items, keyHint{"Shift+D", i18n.T("Hepsini çöpe")})
				}
			case rowFolder:
				if r.acct >= 0 && r.acct < len(m.accts) {
					f := m.accts[r.acct].folder(r.folder)
					if emptyable(f) {
						items = append(items, keyHint{"Shift+D", i18n.T("Boşalt")})
					} else {
						items = append(items, keyHint{"Shift+D", i18n.T("Hepsini çöpe")})
					}
				}
			}
		}
		keybar(g, g.H-1, items, i18n.T("? Yardım"), &m.hits)
		return
	}
	if m.search != nil && m.search.focused {
		keybar(g, g.H-1, withComposeHint([]keyHint{{"Enter", i18n.T("Aç")}, {"Tab", "Kapsam"}, {"Esc", i18n.T("Kapat")}}), i18n.T("? Yardım"), &m.hits)
		return
	}
	if m.focus == focusReader {
		var p *mailmsg.Parsed
		if m.pv != nil {
			p = m.body(m.pv.key)
		}
		keybar(g, g.H-1, m.withSelKeys(withComposeHint(readerKeys(p, true, m.pv != nil && m.pv.attMode))), i18n.T("? Yardım"), &m.hits)
		return
	}
	del := i18n.T("Sil")
	trash := m.inTrashView()
	if trash {
		del = i18n.T("Kalıcı sil")
	}
	if m.onGroupHeader() {
		del = i18n.T("Göndereni sil")
		items := []keyHint{{"Enter", i18n.T("Aç")}, {"d", del}}
		if trash {
			items = append(items, keyHint{"m", i18n.T("Geri yükle")})
		}
		items = append(items, keyHint{"Shift+G", i18n.T("Düz liste")}, keyHint{"/", i18n.T("Ara")})
		keybar(g, g.H-1, withComposeHint(items), i18n.T("? Yardım"), &m.hits)
		return
	}
	items := []keyHint{{"→/Enter", i18n.T("Aç")}, {"t", i18n.T("Sekmede aç")}, {"n", i18n.T("Yeni")}, {"r", i18n.T("Yanıtla")}, {"d", del}, {"m", i18n.T("Klasöre taşı")}, {"k", i18n.T("Kaydet")}, {"e", i18n.T("Ekler")}, {"o", i18n.T("Tarayıcıda aç")}, {"/", i18n.T("Ara")}}
	keybar(g, g.H-1, withComposeHint(items), i18n.T("? Yardım"), &m.hits)
}

func (m *Model) drawFolders(g *grid.Grid, x0, top, w, h int) {
	focused := m.focus == focusFolders
	panel(g, x0, top, w, h, focused, cPanel, i18n.T("Klasörler"))
	rows := m.folderRows()
	sel := m.folderRowsSelectable()
	var cur frow
	if focused && m.fcursor < len(sel) {
		cur = sel[m.fcursor]
	}
	bottomReserved := 3
	maxY := top + h - 1 - bottomReserved
	// scroll so the cursor row stays visible
	curIdx := -1
	for i, r := range rows {
		if m.newFolder != nil && r.kind == rowNewFolder && r.acct == m.newFolder.acct {
			curIdx = i
			continue
		}
		if focused && r.kind == cur.kind && r.acct == cur.acct && r.folder == cur.folder && r.read == cur.read && (r.kind == rowAcct || r.kind == rowFolder || r.kind == rowUnified || r.kind == rowSaved) {
			curIdx = i
		}
	}
	visibleRows := maxY - (top + 2)
	start := 0
	if curIdx >= visibleRows {
		start = curIdx - visibleRows + 1
	}
	m.hits.add(hit{x: x0, y: top, w: w, h: h, kind: hitFolderPane})
	selIdx := make([]int, len(rows)) // row -> index among the selectable rows
	for i, n := 0, 0; i < len(rows); i++ {
		selIdx[i] = -1
		if k := rows[i].kind; k == rowAcct || k == rowFolder || k == rowUnified || k == rowSaved {
			selIdx[i] = n
			n++
		}
	}
	y := top + 2
	for i := start; i < len(rows) && y < maxY; i++ {
		r := rows[i]
		isCur := i == curIdx
		if selIdx[i] >= 0 {
			m.hits.add(hit{x: x0 + 1, y: y, w: w - 2, h: 1, kind: hitFolderRow, idx: selIdx[i]})
		}
		switch r.kind {
		case rowAcct:
			a := m.accts[r.acct]
			arrow, afg := "▸", cDim
			fg := cDim
			if a.Open {
				arrow, afg, fg = "▾", cFG, cFG
			}
			var bg string
			if isCur {
				g.Fill(x0+1, y, w-2, 1, cSelFocus)
				bg = cSelFocus
				fg = cFG
			}
			if isCur {
				g.Put(x0+2, y, "▌", grid.Style{FG: cAccent, BG: bg}, 0)
			} else {
				g.Put(x0+2, y, arrow, grid.Style{FG: afg, BG: bg}, 0)
			}
			room := w - 7
			if r.count > 0 {
				room -= len(fmt.Sprint(r.count)) + 1
			}
			g.Put(x0+4, y, r.label, grid.Style{FG: fg, BG: bg, Bold: a.Open || isCur}, room)
			if r.count > 0 {
				g.RPut(x0+w-2, y, fmt.Sprint(r.count), grid.Style{FG: cYellow, BG: bg, Bold: true})
			}
		case rowKindLine:
			g.Put(x0+6, y, r.label, st(cYellow), w-8)
		case rowHeading:
			g.Put(x0+6, y, r.label, st(cFaint), w-8)
		case rowSep:
			g.Put(x0+1, y, strings.Repeat("─", w-2), st(cBorder), 0)
		case rowBlank:
		case rowUnified, rowSaved:
			active := r.kind == rowUnified && m.virt == viewUnified && r.read == m.readView || r.kind == rowSaved && m.virt == viewSaved
			var bg string
			if isCur {
				g.Fill(x0+1, y, w-2, 1, cSelFocus)
				bg = cSelFocus
			} else if active {
				g.Fill(x0+1, y, w-2, 1, cSel)
				bg = cSel
			}
			if active {
				g.Put(x0+1, y, "▌", grid.Style{FG: cAccent, BG: bg}, 0)
			}
			fg := cFG
			if r.kind == rowSaved {
				g.Put(x0+2, y, "★", grid.Style{FG: cAccent, BG: bg}, 0)
				g.Put(x0+4, y, savedTitle(), grid.Style{FG: fg, BG: bg, Bold: active || isCur}, w-10)
			} else {
				g.Put(x0+2, y, r.label, grid.Style{FG: fg, BG: bg, Bold: active || isCur}, w-8)
			}
			if r.count > 0 {
				cc := cYellow
				if r.kind == rowSaved {
					cc = cDim
				}
				g.RPut(x0+w-2, y, fmt.Sprint(r.count), grid.Style{FG: cc, BG: bg, Bold: r.kind == rowUnified})
			}
		case rowNewFolder:
			bg := cSelFocus
			g.Fill(x0+1, y, w-2, 1, bg)
			g.Put(x0+6, y, "▸ ", grid.Style{FG: cAccent, BG: bg}, 0)
			if m.newFolder != nil {
				cancelW := 2
				m.newFolder.in.Draw(g, x0+8, y, max(8, w-11-cancelW), 1, editStyle(bg))
				cx := x0 + w - 3
				g.Put(cx, y, "✕", grid.Style{FG: cFaint, BG: bg}, 0)
				m.hits.add(hit{x: cx - 1, y: y, w: 3, h: 1, kind: hitFolderCancel})
			}
		case rowFolder:
			active := m.virt == viewFolder && r.acct == m.cur && r.folder == m.folder && r.read == m.readView
			var bg string
			if isCur {
				g.Fill(x0+1, y, w-2, 1, cSelFocus)
				bg = cSelFocus
			} else if active {
				g.Fill(x0+1, y, w-2, 1, cSel)
				bg = cSel
			}
			if active {
				g.Put(x0+2, y, "▌", grid.Style{FG: cAccent, BG: bg}, 0)
			}
			fg := cDim
			if active || isCur {
				fg = cFG
			}
			g.Put(x0+6, y, r.label, grid.Style{FG: fg, BG: bg, Bold: active || isCur}, w-12)
			if r.count > 0 {
				cc := cFaint
				if active || isCur {
					cc = cYellow
				}
				g.RPut(x0+w-3, y, fmt.Sprint(r.count), grid.Style{FG: cc, BG: bg, Bold: active})
			}
		}
		y++
	}
	by := top + h - 1 - bottomReserved
	g.Put(x0+1, by, strings.Repeat("─", w-2), st(cBorder), 0)
	n := len(m.opt.Blocks.All())
	blk := i18n.T("⊘ Engellenenler")
	special := func(yy int, kind rowKind, text, hint string) {
		var bg string
		if focused && cur.kind == kind {
			g.Fill(x0+1, yy, w-2, 1, cSelFocus)
			bg = cSelFocus
		}
		g.Put(x0+2, yy, text, grid.Style{FG: cAccent, BG: bg}, w-4)
		if hint != "" {
			g.RPut(x0+w-2, yy, hint, grid.Style{FG: cFaint, BG: bg})
		}
	}
	cnt := ""
	if n > 0 {
		cnt = fmt.Sprint(n)
	}
	if cnt != "" {
		blk += " " + cnt
	}
	special(by+1, rowBlocked, blk, "Ctrl+B")
	special(by+2, rowAddAcct, i18n.T("+ Hesap ekle"), "+")
	nsel := len(m.folderRowsSelectable())
	m.hits.add(hit{x: x0 + 1, y: by + 1, w: w - 2, h: 1, kind: hitFolderRow, idx: nsel - 2})
	m.hits.add(hit{x: x0 + 1, y: by + 2, w: w - 2, h: 1, kind: hitFolderRow, idx: nsel - 1})
}

// listTitle is "Kişisel · Gelen Kutusu" (or the merged view's name).
func (m *Model) listTitle() string {
	switch m.virt {
	case viewUnified:
		if m.readView {
			return readTitle() + i18n.T(" · tüm hesaplar")
		}
		return unifiedTitle()
	case viewSaved:
		return savedTitle() + i18n.T(" · tüm hesaplar")
	}
	a := m.acct()
	if a == nil {
		return ""
	}
	fl := i18n.T("Gelen Kutusu")
	if f := a.folder(m.folder); f != nil {
		fl = strings.TrimSpace(f.Label())
	}
	if m.readView {
		fl = readTitle()
	}
	return a.Cfg.Name + " · " + fl
}

func (m *Model) drawList(g *grid.Grid, x0, top, w, h int) {
	a := m.acct()
	focused := m.focus == focusList
	if m.search != nil && (m.search.focused || m.search.query() != "") {
		panel(g, x0, top, w, h, focused, cBG, m.searchTitle())
		m.hits.add(hit{x: x0, y: top, w: w, h: h, kind: hitListPane})
		m.drawSearchList(g, x0, top, w, h, focused)
		return
	}
	panel(g, x0, top, w, h, focused, cBG, m.listTitle())
	m.hits.add(hit{x: x0, y: top, w: w, h: h, kind: hitListPane})
	mx, mw := x0+2, w-4
	// search row
	sbg := cRaised
	g.Fill(mx, top+1, mw, 1, sbg)
	lab := i18n.T("Ara: ")
	lx := g.Put(mx+1, top+1, lab, stbg(cFaint, sbg), 0)
	g.Put(lx, top+1, i18n.T("gönderen, konu veya içerik…"), stbg(cFaint, sbg), mx+mw-lx-2)
	g.RPut(mx+mw-1, top+1, "/", stbg(cDim, sbg))
	m.hits.add(hit{x: mx, y: top + 1, w: mw, h: 1, kind: hitSearch, ox: lx, oy: top + 1})
	v := m.visible()
	base := v
	if m.groupsOn() {
		base = m.baseList()
	}
	unread := 0
	for _, e := range base {
		if !e.Seen {
			unread++
		}
	}
	info := i18n.Tf("%d ileti · %d okunmamış", len(base), unread)
	if a != nil {
		if f := a.folder(m.folder); f != nil && m.virt == viewFolder && f.Total > len(base) && !m.inboxView() {
			info = i18n.Tf("%d ileti · %d okunmamış", f.Total, f.Unread)
		}
	}
	// Gelen Kutusu lists every mail; unread vs read is typography, not a split.
	if have, total, loading := m.listProgress(); loading && !m.searching() {
		info = i18n.Tf("Yükleniyor %d/%d", have, total)
	}
	if p := m.pendingF; p != nil {
		info = p.label + i18n.T(" yükleniyor…") // the old list stays until it arrives
	}
	g.Put(mx, top+2, info, st(cFaint), mw)
	g.HLine(x0+1, top+3, w-2, cBorder, "", "")
	y0 := top + 4
	rowsH := top + h - 1 - y0
	if a != nil && m.virt == viewFolder && !a.Loaded[m.folder] && len(v) == 0 {
		msg := i18n.T("Yükleniyor…")
		if a.State == stNeedsLogin {
			msg = i18n.T("Bu hesap için yeniden giriş gerekiyor (+ tuşu).")
		} else if a.Err != "" {
			msg = a.Err
		}
		g.Put(mx, y0+1, msg, st(cFaint), mw)
		return
	}
	if len(v) == 0 {
		m.cursor, m.offset = 0, 0
		msg := "Burada ileti yok."
		if m.virt == viewSaved {
			msg = i18n.T("Kaydedilen ileti yok. Bir iletide k tuşuna basarak sonraya sakla.")
		}
		g.Put(mx, y0+1, msg, st(cFaint), mw)
		return
	}
	m.clampCursorTo(len(v))
	if !m.freeScroll { // keyboard: the cursor row is always in view
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+rowsH {
			m.offset = m.cursor - rowsH + 1
		}
	}
	m.offset = max(0, min(m.offset, max(0, len(v)-rowsH)))
	grouped := m.groupsOn()
	for i := m.offset; i < len(v) && i-m.offset < rowsH; i++ {
		e := v[i]
		y := y0 + i - m.offset
		m.hits.add(hit{x: x0 + 1, y: y, w: w - 2, h: 1, kind: hitListRow, idx: i})
		if grouped && isGroupRow(e) {
			m.drawGroupRow(g, x0, w, mx, mw, y, e, i == m.cursor, focused)
			continue
		}
		var bg string
		if i == m.cursor {
			bg = cSelFocus // always a clear bar, even when another pane has focus
			g.Fill(x0+1, y, w-2, 1, bg)
		}
		indent := 0
		if grouped && i < len(m.grpChild) && m.grpChild[i] {
			indent = 4
		}
		m.drawMailRow(g, mx, mw, y, i, e, bg, indent, m.virt != viewFolder, nil)
	}
}

// drawMailRow draws one mail line; returns where the sender and the subject
// start. needles (search) are highlighted.
func (m *Model) drawMailRow(g *grid.Grid, mx, mw, y, i int, e backend.Envelope, bg string, indent int, multi bool, needles []string) (int, int) {
	unr := !e.Seen
	fg := cDim
	if unr {
		fg = cFG
	}
	dot := " "
	dotc := cAccent
	if unr {
		dot = "●"
	}
	if m.isNew(e) {
		dot, dotc = "●", cGreen
	}
	x := mx + indent
	g.Put(x, y, dot, grid.Style{FG: dotc, BG: bg}, 0)
	subjW := mw - 18 - 10 - indent
	fx, fw, sx, sw := x+2, 15, x+18, subjW
	if multi { // merged views: short coloured account label first
		g.Put(x+2, y, shortName(m.accts[e.Acct].Cfg.Name), grid.Style{FG: acctColor(e.Acct), BG: bg, Bold: true}, 7)
		fx, fw, sx, sw = x+10, 13, x+24, subjW-6
	}
	if e.Flagged {
		sx, sw = sx+2, sw-2
		g.Put(sx-2, y, "★", grid.Style{FG: cAccent, BG: bg}, 0)
	}
	rt := m.rowText(e, fw, sw)
	g.PutClean(fx, y, rt.from, grid.Style{FG: fg, BG: bg, Bold: unr})
	g.PutClean(sx, y, rt.subj, grid.Style{FG: fg, BG: bg, Bold: unr && i == m.cursor})
	if len(needles) > 0 {
		searchHL(g, fx, y, rt.from, needles)
		searchHL(g, sx, y, rt.subj, needles)
	}
	if e.Attach {
		g.Put(mx+mw-8, y, "▪", grid.Style{FG: cPurple, BG: bg}, 0)
	}
	dc := cDim
	if unr {
		dc = cFG
	}
	g.RPut(mx+mw, y, mailmsg.ShortDate(e.Date, m.opt.Now()), grid.Style{FG: dc, BG: bg})
	return fx, sx
}

func (m *Model) isNew(e backend.Envelope) bool {
	ai, fo := m.loc(e)
	for _, x := range m.toasts.Mails() {
		if x.Account == ai && x.Folder == fo && x.UID == e.UID {
			return true
		}
	}
	return false
}

// drawMessage draws headers + attachments + body into a region; returns the
// y where the body starts. Used by the preview pane and the reader tab.
// attFocus: the attachment row has the keyboard (attachment mode); onAtt,
// when set, is told where each attachment was drawn (for mouse clicks).
func drawHeaders(g *grid.Grid, x, y, w int, e backend.Envelope, p *mailmsg.Parsed, attSel int, showCc bool, hl []string, attFocus bool, onAtt func(i, y int)) int {
	kv := func(k, v string, bold bool) {
		g.Put(x, y, fmt.Sprintf("%-7s", k), st(cFaint), 0)
		g.Put(x+8, y, v, grid.Style{FG: cFG, Bold: bold}, w-8)
		if len(hl) > 0 {
			searchHL(g, x+8, y, grid.Truncate(v, w-8), hl)
		}
		y++
	}
	from := e.From
	if e.FromAddr != "" && e.FromAddr != e.From {
		from = e.From + " <" + e.FromAddr + ">"
	}
	to := ""
	if p != nil {
		if len(p.From) > 0 {
			from = mailmsg.FormatAddresses(p.From[:1])
		}
		to = mailmsg.FormatAddresses(p.To)
	}
	kv(i18n.T("Kimden"), from, false)
	kv(i18n.T("Kime"), to, false)
	if showCc && p != nil && len(p.Cc) > 0 {
		kv(i18n.T("Bilgi"), mailmsg.FormatAddresses(p.Cc), false)
	}
	kv(i18n.T("Tarih"), mailmsg.LongDate(e.Date), false)
	kv(i18n.T("Konu"), trimSubj(e.Subject), true)
	if p != nil && len(p.Attachments) > 0 {
		// one line; the list itself is on the "Ekler ve Bağlantılar" tab
		g.Put(x, y, fmt.Sprintf("%-7s", "Ek"), st(cFaint), 0)
		text := fmt.Sprintf("%d dosya", len(p.Attachments))
		if len(p.Attachments) == 1 {
			text = p.Attachments[0].Name + "  (" + mailmsg.HumanSize(p.Attachments[0].Size) + ")"
		}
		g.Put(x+8, y, text, grid.Style{FG: cPurple, Bold: attFocus}, w-8)
		if len(hl) > 0 {
			searchHL(g, x+8, y, grid.Truncate(text, w-8), hl)
		}
		if onAtt != nil {
			onAtt(0, y)
		}
		y++
		_ = attSel
	}
	return y
}

func (m *Model) drawPreview(g *grid.Grid, x0, top, w, h int) {
	focused := m.focus == focusReader
	e, ok := m.selected()
	bc, title := cBorder, i18n.T("Okuma")
	if focused {
		bc = cActive
		if !ok {
			title = i18n.T("Okuma · iletinin içindesin")
		}
	}
	panel(g, x0, top, w, h, focused, cBG, title)
	m.hits.add(hit{x: x0, y: top, w: w, h: h, kind: hitPreviewPane})
	if !ok {
		m.groupPreview(g, x0+2, top+2, w-4)
		return
	}
	ai, fo := m.loc(e)
	k := bodyKey{ai, fo, e.UID}
	r := m.pv
	var hl []string
	if m.searching() {
		hl = m.search.q.Needles()
	}
	if r == nil || r.key != k {
		r = &reader{key: k, env: e, inline: true}
	}
	rx, rw := x0+2, w-4
	p := m.body(k)
	attSel := -1
	if focused && p != nil && len(p.Attachments) > 0 {
		attSel = min(r.att, len(p.Attachments)-1)
	}
	m.drawReaderTabs(g, rx, top+1, rw, r, p)
	g.HLine(x0+1, top+2, w-2, bc, "", "")
	if r.attMode {
		bottom := top + h - 2
		m.drawAttTab(g, rx, top+4, rw, bottom-1, r, p, focused)
		g.Put(rx, top+h-2, i18n.T("Enter: aç · s: İndirilenler'e kaydet · e: Mail"), st(cFaint), rw-6)
		return
	}
	y := drawHeaders(g, rx, top+4, rw, e, p, attSel, false, hl, false, func(i, ay int) {
		m.hits.add(hit{x: rx, y: ay, w: rw, h: 1, kind: hitAttachment, idx: i})
	})
	g.HLine(x0+1, y, w-2, bc, "", "")
	if r.showAtt && p != nil && r.att < len(p.Attachments) {
		g.Put(rx, y, " Ek: "+p.Attachments[r.att].Name+" ", stb(cPurple), rw)
	}
	y += 2
	bottom := top + h - 2
	r.lastH = max(1, bottom-1-y)
	hint, hintFG := i18n.T("Enter: iletiye gir · o: tarayıcıda aç"), cFaint
	if focused {
		hint, hintFG = i18n.T("Enter: yanıtla  ·  ]: sonraki  ·  [: önceki"), cDim
	}
	if p != nil && (len(p.Links) > 0 || len(p.Attachments) > 0) {
		if focused {
			hint += " · e: ekler"
		} else {
			hint += " · E: ekler"
		}
	}
	switch {
	case p != nil:
		text := viewText(p)
		links := p.Links
		if r.showAtt && r.att < len(p.Attachments) {
			text, links = string(p.Attachments[r.att].Data), nil
		}
		if strings.TrimSpace(text) == "" {
			text = i18n.T("(Bu iletide metin yok.)")
		}
		lines, paras := m.wrapped(k, text, rw)
		if r.scroll > max(0, len(lines)-r.lastH) {
			r.scroll = max(0, len(lines)-r.lastH)
		}
		m.tv = textView{ok: true, x: rx, y: y, w: rw, h: max(1, bottom-1-y), lines: lines, paras: paras, r: r, key: k, att: r.showAtt}
		for i := r.scroll; i < len(lines) && y < bottom-1; i++ {
			fg := cFG
			if strings.HasPrefix(lines[i].text, ">") {
				fg = cFaint
			}
			m.drawBodyLine(g, rx, y, lines[i].text, fg, rw, links)
			if len(hl) > 0 {
				hlCells(g, rx, y, lines[i].text, hl, bodyCellW)
			}
			y++
		}
		m.paintSel(g)
		m.paintBodyFind(g)
		if len(lines) > r.lastH {
			pct := 100
			if len(lines)-r.lastH > 0 {
				pct = min(100, r.scroll*100/(len(lines)-r.lastH))
			}
			g.RPut(x0+w-3, top+h-2, strconv.Itoa(pct)+"%", st(cFaint))
		}
	case m.bodyErr[k] != "":
		g.Put(rx, y, m.bodyErr[k], st(cRed), rw)
	default:
		g.Put(rx, y, i18n.T("Yükleniyor…"), st(cFaint), rw)
	}
	if m.bodyFind != nil {
		m.drawBodyFind(g, rx, top+h-2, rw)
	} else {
		g.Put(rx, top+h-2, hint, st(hintFG), rw-6)
	}
}

// ---- toasts ----

func (m *Model) drawToasts(g *grid.Grid) {
	tw := min(44, g.W-4)
	tx := g.W - tw - 2
	bottom := g.H - 5 // leave room above the panel border + key bar
	y := bottom
	if m.toasts.HasMail() {
		mails := m.toasts.Mails()
		shown := len(mails)
		if shown > 8 {
			shown = 8
		}
		lines := shown
		if len(mails) == 1 {
			lines = 2 // from + subject
		} else if len(mails) > shown {
			lines++ // "… ve N ileti daha"
		}
		th := lines + 4
		ty := bottom - th + 1
		g.Box(tx, ty, tw, th, cToastBd, cToastBG, "", "")
		g.Put(tx+2, ty+1, "● "+m.toasts.Title(), stb(cGreen), 0)
		acctLabel := mails[0].AccountName
		for _, x := range mails {
			if x.AccountName != acctLabel {
				acctLabel = ""
			}
		}
		if acctLabel != "" {
			g.RPut(tx+tw-2, ty+1, grid.Truncate(acctLabel, tw-18), st(cDim))
		}
		if len(mails) == 1 {
			g.Put(tx+2, ty+2, tidy(mails[0].From), stb(cFG), tw-4)
			g.Put(tx+2, ty+3, trimSubj(mails[0].Subject), st(cDim), tw-4)
			m.hits.add(hit{x: tx + 1, y: ty + 2, w: tw - 2, h: 2, kind: hitMailToastItem, idx: 0})
		} else {
			for i := 0; i < shown; i++ {
				yy := ty + 2 + i
				num := strconv.Itoa(i + 1)
				cx := g.Put(tx+2, yy, num+" ", st(cAccent), 0)
				fx := g.Put(cx, yy, grid.Truncate(tidy(mails[i].From), 12), stb(cFG), 0)
				g.Put(fx+1, yy, "– "+trimSubj(mails[i].Subject), st(cDim), tx+tw-2-fx-1)
				m.hits.add(hit{x: tx + 1, y: yy, w: tw - 2, h: 1, kind: hitMailToastItem, idx: i})
			}
			if len(mails) > shown {
				g.Put(tx+2, ty+2+shown, i18n.Tf("… ve %d ileti daha", len(mails)-shown), st(cFaint), 0)
			}
		}
		hint := i18n.T("tıkla: aç")
		if len(mails) > 1 {
			hint = i18n.T("tıkla / 1-") + strconv.Itoa(min(9, len(mails))) + i18n.T(": aç")
		}
		g.Put(tx+2, ty+th-2, hint, st(cFaint), 0)
		g.RPut(tx+tw-2, ty+th-2, i18n.T("x: kapat"), st(cFaint))
		m.hits.add(hit{x: tx + tw - 10, y: ty + th - 2, w: 9, h: 1, kind: hitMailToastClose})
		y = ty - 1
	}
	acts := m.toasts.Actions()
	for i := len(acts) - 1; i >= 0; i-- {
		a := acts[i]
		body, extra := toastBody(a)
		maxW := g.W - 4
		if maxW < 20 {
			break
		}
		undo := a.Kind == toast.Deleted
		inner := max(16, maxW-6)
		if undo {
			inner = max(16, maxW-16) // room for "u geri al" if it were inline; we use a row
		}
		lines := wrapText(body, inner)
		if extra != "" {
			lines = append(lines, extra)
		}
		if len(lines) == 0 {
			lines = []string{""}
		}
		lw := 0
		for _, l := range lines {
			if n := grid.Width(l); n > lw {
				lw = n
			}
		}
		if undo && lw < 18 {
			lw = 18
		}
		w := min(maxW, max(24, lw+6))
		if w-6 < inner {
			lines = wrapText(body, w-6)
			if extra != "" {
				lines = append(lines, extra)
			}
		}
		th := len(lines) + 2
		if undo {
			th++
		}
		ty := y - th + 1
		if ty < 3 {
			break
		}
		x := g.W - w - 2
		bd, bg := cBorder, cRaised
		if a.Kind == toast.Error {
			bd, bg = cErrBd, cErrBG
		}
		g.Box(x, ty, w, th, bd, bg, "", "")
		g.Put(x+w-3, ty, "×", stbg(cDim, bg), 0)
		m.hits.add(hit{x: x, y: ty, w: w, h: th, kind: hitToast})
		m.hits.add(hit{x: x + w - 4, y: ty, w: 4, h: 1, kind: hitToastClose, idx: a.ID})
		for li, l := range lines {
			yy := ty + 1 + li
			prefix := ""
			fg, bold := cFG, false
			if li == 0 {
				switch a.Kind {
				case toast.Success:
					prefix, fg = "✓ ", cGreen
				case toast.Error:
					prefix, fg, bold = "! ", cRed, true
				}
			}
			cx := x + 2
			if prefix != "" {
				cx = g.Put(cx, yy, prefix, st(fg), 0)
			}
			g.Put(cx, yy, l, stbgb(cFG, bg, bold), w-2-(cx-x))
		}
		if undo {
			g.Put(x+2, ty+th-2, " u ", stbgb(cFG, cKey, true), 0)
			g.Put(x+6, ty+th-2, "geri al", st(cAccent), 0)
		}
		y = ty - 1
	}
}

// toastBody is the full, untruncated text of an action toast.
func toastBody(a toast.Action) (body, extra string) {
	switch a.Kind {
	case toast.Deleted:
		if a.Count > 1 {
			body = a.Text
		} else if a.Label != "" {
			body = strings.TrimSpace(a.Label + " " + a.Text)
		} else {
			body = a.Text
		}
		return body, ""
	case toast.Success:
		if a.Label != "" {
			return strings.TrimSpace(a.Label + " " + a.Text), ""
		}
		return a.Text, ""
	default:
		return a.Text, ""
	}
}
