package ui

import (
	"os"
	"strings"
	"time"

	"termail/internal/search"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/edit"
	"termail/internal/grid"
)

// Mouse support. Every frame records where the clickable things are (a hit
// map built while drawing); a click is looked up there, so what the mouse
// does always matches what is on screen. The keyboard is unaffected.

type hitKind int

const (
	hitModal          hitKind = iota + 1 // an overlay covers everything below
	hitFolderPane                        // folder panel background
	hitListPane                          // message list background
	hitPreviewPane                       // reading pane (beside / below the list)
	hitReaderPane                        // a mail opened in its own tab
	hitFolderRow                         // idx = selectable folder row
	hitListRow                           // idx = index into visible()
	hitTab                               // idx = tab index
	hitTabClose                          // idx = tab index (the small ×)
	hitField                             // idx = compose field
	hitLink                              // s = address
	hitToastClose                        // idx = toast id
	hitMailToastShow                     // new-mail toast: show
	hitMailToastClose                    // new-mail toast: close
	hitLinkRow                           // idx = row in the link picker
	hitAcctRow                           // idx = row in the account picker
	hitToast                             // a toast (absorbs clicks)
	hitSearch                            // the search row above the list
	hitScope                             // the search scope label (Bu klasör / ...)
	hitRecent                            // idx = recent search
	hitChip                              // idx = filter hint (search.FilterHints())
	hitAttachment                        // the "Ek: N dosya" header line: opens the second tab
	hitReaderTab                         // idx 0 = Mail, 1 = Ekler ve Bağlantılar
	hitTabItem                           // idx = row of the second tab
	hitPreviewHead                       // the Okuma title row (double-click toggles wide mode)
	hitAcctChip                          // idx -1 = F1 Tüm Gelen Kutuları, else account index
	hitMailToastItem                     // idx = entry in toasts.Mails()
	hitKeybar                            // s = key string
	hitCtxItem                           // idx = context menu row
	hitMailFind                          // in-mail search field
	hitFolderCancel                      // ✕ on the inline new/rename folder field
	hitConfirmYes                        // confirm popup: confirm button
	hitConfirmNo                         // confirm popup: cancel button
	hitConfirmAlt                        // confirm popup: middle button (e.g. Kaydetme)
	hitFromRow                           // compose From row (opens the account list)
	hitFromPrev                          // compose From: previous account
	hitFromNext                          // compose From: next account
	hitFromAcct                          // idx = account in the From dropdown
	hitSuggest                           // idx = recipient suggestion
	hitPickerItem                        // idx = attachment picker row
	hitPickerQuick                       // s = folder path
	hitPickerPath                        // attachment picker path field
)

type hit struct {
	x, y, w, h int
	kind       hitKind
	idx        int
	s          string
	ox, oy     int // text fields: where the text starts
}

type hitMap struct{ list []hit }

func (hm *hitMap) reset()    { hm.list = hm.list[:0] }
func (hm *hitMap) add(h hit) { hm.list = append(hm.list, h) }

// at returns the top-most target under (x, y): later entries were drawn on
// top. A modal overlay hides everything drawn before it.
func (hm *hitMap) at(x, y int) (hit, bool) {
	for i := len(hm.list) - 1; i >= 0; i-- {
		h := hm.list[i]
		if x >= h.x && x < h.x+h.w && y >= h.y && y < h.y+h.h {
			if h.kind == hitModal {
				return h, false
			}
			return h, true
		}
	}
	return hit{}, false
}

const (
	doubleClick = 400 * time.Millisecond
	wheelStep   = 3
)

type lastClick struct {
	at   time.Time
	kind hitKind
	idx  int
}

// clickRun counts quick clicks on the same cell (double / triple click).
type clickRun struct {
	at   time.Time
	x, y int
	n    int
}

func (m *Model) clickCount(x, y int) int {
	now := m.opt.Now()
	c := &m.clickN
	if c.n > 0 && c.x == x && c.y == y && now.Sub(c.at) < doubleClick {
		c.n++
	} else {
		c.n = 1
	}
	c.at, c.x, c.y = now, x, y
	return c.n
}

// fieldDrag: the left button went down in a text field and is still held.
type fieldDrag struct {
	on     bool
	idx    int // compose field, or -1: the search box
	ox, oy int
	h      int
}

// onMouse handles one mouse event. Pure pointer motion (with no button
// held) changes nothing, so it does not even build a new frame.
func (m *Model) onMouse(ev tea.MouseMsg) tea.Cmd {
	if m.login != nil {
		if ev.Action == tea.MouseActionPress && ev.Button == tea.MouseButtonRight && !m.login.busy {
			return m.pasteInto(m.login.field()) // right-click pastes, like in a terminal
		}
		if ev.Action == tea.MouseActionPress && ev.Button == tea.MouseButtonLeft {
			// the sign-in button and the key bar are clickable
			if h, ok := m.hits.at(ev.X, ev.Y); ok && h.kind == hitKeybar && h.s != "" {
				return m.onKey(asKey(h.s))
			}
		}
		m.noRender = true
		return nil
	}
	switch {
	case ev.Action == tea.MouseActionPress && ev.Button == tea.MouseButtonWheelUp:
		return m.wheel(ev.X, ev.Y, -wheelStep)
	case ev.Action == tea.MouseActionPress && ev.Button == tea.MouseButtonWheelDown:
		return m.wheel(ev.X, ev.Y, wheelStep)
	case ev.Action == tea.MouseActionPress && ev.Button == tea.MouseButtonLeft:
		return m.press(ev.X, ev.Y, ev.Shift)
	case ev.Action == tea.MouseActionPress && ev.Button == tea.MouseButtonRight:
		return m.rightClick(ev.X, ev.Y)
	case ev.Action == tea.MouseActionPress && ev.Button == tea.MouseButtonMiddle:
		return m.middleClick(ev.X, ev.Y)
	case ev.Action == tea.MouseActionMotion && ev.Button == tea.MouseButtonLeft:
		return m.drag(ev.X, ev.Y)
	case ev.Action == tea.MouseActionMotion:
		return m.hoverAt(ev.X, ev.Y)
	case ev.Action == tea.MouseActionRelease:
		m.fdrag.on = false
		if m.rsel != nil && m.rsel.dragging {
			return m.textRelease()
		}
	}
	m.noRender = true
	return nil
}

func hoverable(k hitKind) bool {
	switch k {
	case hitTab, hitTabClose, hitFolderRow, hitListRow, hitPreviewHead,
		hitAttachment, hitReaderTab, hitTabItem, hitLink, hitLinkRow,
		hitAcctRow, hitSearch, hitScope, hitRecent, hitChip,
		hitToastClose, hitMailToastShow, hitMailToastClose, hitAcctChip, hitMailToastItem,
		hitKeybar, hitCtxItem, hitFolderCancel, hitConfirmYes, hitConfirmNo, hitConfirmAlt,
		hitFromRow, hitFromPrev, hitFromNext, hitFromAcct, hitSuggest,
		hitPickerItem, hitPickerQuick, hitPickerPath:
		return true
	}
	return false
}

func hoverSame(a, b hit) bool {
	return a.kind == b.kind && a.idx == b.idx && a.x == b.x && a.y == b.y && a.w == b.w && a.h == b.h && a.s == b.s
}

// hoverAt tracks the clickable target under the pointer and only asks
// for a new frame when that target changes.
func (m *Model) hoverAt(x, y int) tea.Cmd {
	m.hoverX, m.hoverY = x, y
	h, ok := m.hits.at(x, y)
	if !ok || !hoverable(h.kind) {
		if !m.hoverOn {
			m.noRender = true
			return nil
		}
		m.hoverOn = false
		return nil
	}
	if m.hoverOn && hoverSame(m.hover, h) {
		m.noRender = true
		return nil
	}
	m.hover, m.hoverOn = h, true
	return nil
}

// resolveHover re-checks the last pointer position against the hits of
// this frame, so a layout change does not leave a stale highlight.
func (m *Model) resolveHover() {
	if !m.hoverOn {
		return
	}
	h, ok := m.hits.at(m.hoverX, m.hoverY)
	if !ok || !hoverable(h.kind) {
		m.hoverOn = false
		return
	}
	m.hover = h
}

func (m *Model) paintHover(g *grid.Grid) {
	if !m.hoverOn || !hoverable(m.hover.kind) {
		return
	}
	g.Hover(m.hover.x, m.hover.y, m.hover.w, m.hover.h, cHover)
}

// middleClick closes an extra tab when the pointer is on its title
// (or its ×). The main Mail tab (index 0) cannot be closed this way.
func (m *Model) middleClick(x, y int) tea.Cmd {
	h, ok := m.hits.at(x, y)
	if !ok {
		m.noRender = true
		return nil
	}
	if h.kind == hitTab || h.kind == hitTabClose {
		if h.idx > 0 {
			return m.closeTab(h.idx)
		}
		m.noRender = true
		return nil
	}
	m.noRender = true
	return nil
}

// press: the left button went down.
func (m *Model) press(x, y int, shift bool) tea.Cmd {
	h, ok := m.hits.at(x, y)
	n := m.clickCount(x, y)
	if m.ctxMenu != nil {
		if ok && h.kind == hitCtxItem {
			return m.ctxMenu.choose(m, h.idx)
		}
		m.ctxMenu = nil
		if !ok || h.kind != hitKeybar {
			return nil
		}
	}
	if ok && h.kind == hitFolderCancel {
		m.newFolder = nil
		return nil
	}
	if ok && m.confirm != nil && h.kind == hitConfirmYes {
		if m.confirm.strict {
			return m.confirm.key(m, "e")
		}
		return m.confirm.key(m, "enter")
	}
	if ok && m.confirm != nil && h.kind == hitConfirmNo {
		return m.confirm.key(m, "esc")
	}
	if ok && m.confirm != nil && h.kind == hitConfirmAlt {
		ak := m.confirm.altKey
		if ak == "" {
			ak = "d"
		}
		return m.confirm.key(m, ak)
	}
	if c := m.tabs[m.active].compose; c != nil {
		switch {
		case ok && h.kind == hitFromPrev:
			if len(m.accts) > 0 {
				c.from = (c.from - 1 + len(m.accts)) % len(m.accts)
			}
			return nil
		case ok && h.kind == hitFromNext:
			if len(m.accts) > 0 {
				c.from = (c.from + 1) % len(m.accts)
			}
			return nil
		case ok && h.kind == hitFromRow:
			c.focus = fFrom
			c.dropdown = !c.dropdown
			c.applyFocus()
			return nil
		case ok && h.kind == hitFromAcct:
			if h.idx >= 0 && h.idx < len(m.accts) {
				c.from = h.idx
			}
			c.dropdown = false
			return nil
		case ok && h.kind == hitSuggest:
			if h.idx >= 0 && h.idx < len(c.suggest) {
				c.suggestCur = h.idx
				c.acceptSuggest()
			}
			return nil
		case ok && h.kind == hitPickerQuick && c.picker != nil:
			c.picker.goDir(h.s)
			return nil
		case ok && h.kind == hitPickerItem && c.picker != nil:
			c.picker.cur = h.idx
			// click selects; a second click / Enter adds or enters
			if n >= 2 && h.idx < len(c.picker.entries) {
				e := c.picker.entries[h.idx]
				if strings.HasSuffix(e, string(os.PathSeparator)) || dirExists(e) {
					c.picker.goDir(strings.TrimRight(e, `/\`))
					return nil
				}
				return c.picker.add(c, e)
			}
			return nil
		case ok && h.kind == hitPickerPath && c.picker != nil:
			c.picker.in.Click(x-h.ox, 0, shift)
			m.fdrag = fieldDrag{on: true, idx: -2, ox: h.ox, oy: h.y, h: 1}
			return nil
		}
		if c.dropdown && !(ok && (h.kind == hitFromRow || h.kind == hitFromAcct || h.kind == hitFromPrev || h.kind == hitFromNext)) {
			c.dropdown = false
			if !ok {
				return nil
			}
		}
	}
	if ok && (h.kind == hitPreviewPane || h.kind == hitReaderPane || h.kind == hitLink) && m.tv.contains(x, y) {
		var cmd tea.Cmd
		link := ""
		if h.kind == hitLink {
			link = h.s // opened on release, unless text was selected
		} else {
			cmd = m.click(x, y, h) // (stepping into the mail, as before)
		}
		m.textPress(x, y, n, link, shift)
		return cmd
	}
	m.clearSel()
	if ok && h.kind == hitField {
		if c := m.tabs[m.active].compose; c != nil {
			return c.pressField(m, h, x, y, shift, n)
		}
	}
	if ok && h.kind == hitSearch {
		cmd := m.openSearch()
		m.search.in.Click(x-h.ox, 0, shift)
		m.fdrag = fieldDrag{on: true, idx: -1, ox: h.ox, oy: h.y, h: 1}
		return cmd
	}
	if ok && h.kind == hitMailFind {
		if m.bodyFind == nil {
			m.openBodyFind()
		}
		if m.bodyFind != nil {
			m.bodyFind.in.Click(x-h.ox, 0, shift)
		}
		return nil
	}
	if !ok {
		m.noRender = true
		return nil
	}
	return m.click(x, y, h)
}

// drag: the pointer moved with the left button held.
func (m *Model) drag(x, y int) tea.Cmd {
	if m.rsel != nil && m.rsel.dragging {
		return m.textDrag(x, y)
	}
	if d := m.fdrag; d.on {
		var e *edit.Editor
		if d.idx == -2 {
			if c := m.tabs[m.active].compose; c != nil && c.picker != nil {
				e = &c.picker.in
			}
		} else if d.idx < 0 && m.search != nil {
			e = &m.search.in
		} else if c := m.tabs[m.active].compose; c != nil {
			e = c.fieldAt(d.idx)
		}
		if e != nil {
			if d.h > 1 && y < d.oy {
				e.ScrollBy(-1)
			} else if d.h > 1 && y >= d.oy+d.h {
				e.ScrollBy(1)
			}
			e.Click(x-d.ox, max(-1, min(y-d.oy, d.h)), true)
			return nil
		}
	}
	m.noRender = true
	return nil
}

// rightClick copies the selected mail text, or pastes into a text field.
func (m *Model) rightClick(x, y int) tea.Cmd {
	h, ok := m.hits.at(x, y)
	if !ok {
		m.noRender = true
		return nil
	}
	switch h.kind {
	case hitField:
		if c := m.tabs[m.active].compose; c != nil {
			if c.focus != h.idx {
				c.focus = h.idx
				c.applyFocus()
			}
			if f := c.field(); f != nil && !c.sending {
				return m.pasteInto(f)
			}
		}
	case hitSearch:
		ocmd := m.openSearch()
		before := m.search.in.Value()
		cmd := m.pasteInto(&m.search.in)
		if m.search.in.Value() == before {
			return tea.Batch(ocmd, cmd)
		}
		return tea.Batch(ocmd, cmd, m.searchChanged())
	case hitPreviewPane, hitReaderPane, hitLink:
		if m.copySelection() { // existing: a live selection is copied
			return nil
		}
		if r := m.activeReader(); r != nil {
			m.showMailMenu(x, y, r.key.acct, r.key.folder, r.env)
		}
		return nil
	case hitListRow:
		v := m.visible()
		if h.idx >= 0 && h.idx < len(v) && !isGroupRow(v[h.idx]) {
			m.cursor = h.idx
			m.focus = focusList
			e := v[h.idx]
			ai, fo := m.loc(e)
			m.showMailMenu(x, y, ai, fo, e)
		}
		return nil
	case hitFolderRow:
		rows := m.folderRowsSelectable()
		if h.idx >= 0 && h.idx < len(rows) {
			m.fcursor = h.idx
			m.focus = focusFolders
			m.showFolderMenu(x, y, rows[h.idx])
		}
		return nil
	}
	m.noRender = true
	return nil
}

func (m *Model) wheel(x, y, d int) tea.Cmd {
	if m.links != nil {
		m.links.cur = max(0, min(len(m.links.links)-1, m.links.cur+d/wheelStep))
		return nil
	}
	h, ok := m.hits.at(x, y)
	if !ok {
		m.noRender = true
		return nil
	}
	if r := m.tabs[m.active].read; r == nil && m.pv != nil && (h.kind == hitPreviewPane || h.kind == hitPreviewHead || h.kind == hitTabItem || h.kind == hitReaderTab) {
		if r = m.pv; r.attMode && m.pv.key == m.selectedKey() {
			return r.wheelTab(m, d)
		}
	} else if r != nil && r.attMode && (h.kind == hitReaderPane || h.kind == hitTabItem || h.kind == hitReaderTab) {
		return r.wheelTab(m, d)
	}
	switch h.kind {
	case hitListRow, hitListPane:
		n := len(m.visible())
		m.offset = max(0, min(m.offset+d, n-1))
		m.freeScroll = true
		return nil
	case hitPreviewPane, hitPreviewHead, hitLink:
		if m.tabs[m.active].kind == tabRead {
			m.tabs[m.active].read.scroll = max(0, m.tabs[m.active].read.scroll+d)
			return nil
		}
		if e, ok := m.selected(); ok && m.pv != nil {
			ai, fo := m.loc(e)
			if m.pv.key == (bodyKey{ai, fo, e.UID}) {
				m.pv.scroll = max(0, m.pv.scroll+d)
				return nil
			}
		}
	case hitField:
		if c := m.tabs[m.active].compose; c != nil && h.idx == fBody {
			c.body.ScrollBy(d)
			return nil
		}
	case hitReaderPane:
		if r := m.tabs[m.active].read; r != nil {
			r.scroll = max(0, r.scroll+d)
			return nil
		}
	case hitFolderRow, hitFolderPane:
		if m.focus == focusFolders {
			k := "down"
			if d < 0 {
				k = "up"
			}
			cmd, _ := m.folderKey(k)
			return cmd
		}
	}
	m.noRender = true
	return nil
}

func (m *Model) click(x, y int, h hit) tea.Cmd {
	now := m.opt.Now()
	dbl := m.lastClk.kind == h.kind && m.lastClk.idx == h.idx && now.Sub(m.lastClk.at) < doubleClick
	m.lastClk = lastClick{at: now, kind: h.kind, idx: h.idx}
	if dbl {
		m.lastClk = lastClick{} // a third click starts over
	}
	switch h.kind {
	case hitKeybar:
		if h.s != "" {
			return m.onKey(asKey(h.s))
		}
		return nil
	case hitCtxItem:
		if m.ctxMenu != nil {
			return m.ctxMenu.choose(m, h.idx)
		}
		return nil
	case hitScope:
		if m.search != nil {
			m.search.scope = (m.search.scope + 1) % len(scopeNames())
			return m.searchChanged()
		}
		return nil
	case hitRecent:
		if m.search != nil && h.idx < len(m.prefs.Recent) {
			return m.applySearch(m.prefs.Recent[h.idx])
		}
		return nil
	case hitChip:
		if m.search != nil && h.idx < len(search.FilterHints()) {
			return m.insertFilter(search.FilterHints()[h.idx])
		}
		return nil
	case hitListRow:
		v := m.visible()
		if h.idx >= len(v) {
			return nil
		}
		if m.search != nil {
			m.search.focused = false
			if m.searching() {
				m.addRecent(m.search.query())
			}
		}
		m.focus = focusList
		m.cursor = h.idx
		if isGroupRow(v[h.idx]) {
			if !dbl { // (a double click is two clicks: open + close would undo it)
				m.setGroupOpen(m.grps[groupKeyOf(v[h.idx])], !m.grps[groupKeyOf(v[h.idx])].open)
			}
			return m.previewCmd()
		}
		cmd := m.previewCmd()
		if dbl {
			e := v[h.idx]
			ai, fo := m.loc(e)
			return tea.Batch(cmd, m.openReader(ai, fo, e))
		}
		return cmd
	case hitFolderRow:
		m.fcursor = h.idx
		m.focus = focusFolders
		rows := m.folderRowsSelectable()
		if h.idx < len(rows) && rows[h.idx].kind == rowFolder {
			r := rows[h.idx]
			m.pendingF = nil
			cmd := m.openFolder(r.acct, r.folder)
			m.focus = focusFolders
			m.fcursor = h.idx
			return cmd
		}
		cmd, _ := m.folderKey("enter")
		if m.login == nil && m.blocks == nil {
			m.focus = focusFolders
		}
		return cmd
	case hitTab:
		m.switchTab(h.idx)
		return nil
	case hitTabClose:
		return m.closeTab(h.idx)
	case hitAttachment, hitReaderTab, hitTabItem: // the reading pane's tabs and the rows of the second one
		var cmd tea.Cmd
		r := m.tabs[m.active].read
		if r == nil && m.pv != nil {
			if m.focus != focusReader {
				cmd = m.enterReader(m.pv.env)
			}
			r = m.pv
		}
		if r == nil {
			return cmd
		}
		switch {
		case h.kind == hitReaderTab && h.idx == 0:
			r.attMode = false
		case h.kind == hitReaderTab || h.kind == hitAttachment:
			if !r.attMode {
				r.gotoAttachments(m)
			}
		default:
			if p := m.body(r.key); p != nil {
				if items := tabItems(p); h.idx < len(items) {
					r.sel = h.idx
					if items[h.idx].att >= 0 {
						r.att = items[h.idx].att
					}
					if dbl {
						return tea.Batch(cmd, r.openItem(m, p, items[h.idx]))
					}
				}
			}
		}
		return cmd
	case hitLink:
		return m.openLink(h.s)
	case hitToastClose:
		m.toasts.Remove(h.idx)
		return nil
	case hitMailToastShow:
		return m.jumpToNewMail()
	case hitMailToastItem:
		mails := m.toasts.Mails()
		if h.idx >= 0 && h.idx < len(mails) {
			return m.jumpToMail(mails[h.idx])
		}
		return nil
	case hitMailToastClose:
		m.toasts.DismissMail()
		return nil
	case hitAcctChip:
		if h.idx < 0 {
			return m.showUnified()
		}
		return m.switchAccount(h.idx)
	case hitLinkRow:
		if m.links != nil && h.idx < len(m.links.links) {
			return m.links.open(m, h.idx)
		}
	case hitAcctRow:
		if m.acctPicker != nil {
			return m.acctPicker.choose(m, h.idx)
		}
	case hitPreviewHead:
		if m.focus != focusReader && m.tabs[m.active].kind == tabList {
			if e, ok := m.selected(); ok {
				return m.enterReader(e)
			}
		}
		return nil
	case hitPreviewPane:
		if m.focus != focusReader && m.tabs[m.active].kind == tabList {
			if e, ok := m.selected(); ok {
				return m.enterReader(e) // like Enter: step into the mail
			}
		}
		return nil
	}
	m.noRender = true
	return nil
}

// pressField focuses a compose field and puts the cursor where it was
// clicked; Shift+click extends the selection, a double click selects a
// word, a triple click the line. Dragging selects (see drag).
func (c *composer) pressField(m *Model, h hit, x, y int, shift bool, clicks int) tea.Cmd {
	if c.picker != nil || c.dropdown {
		return nil
	}
	if c.focus != h.idx {
		c.focus = h.idx
		c.applyFocus()
		shift = false
	}
	f := c.field()
	if f == nil {
		return nil
	}
	ox := h.ox
	if x < ox {
		x = ox
	}
	f.Click(x-ox, y-h.oy, shift)
	switch {
	case clicks == 2:
		f.Select(f.WordAt(f.Cursor()))
	case clicks >= 3:
		f.Select(f.LineAt(f.Cursor()))
	}
	hh := 1
	if h.idx == fBody {
		hh = h.h
	}
	m.fdrag = fieldDrag{on: clicks == 1, idx: h.idx, ox: ox, oy: h.oy, h: hh}
	return nil
}
