package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/blocklist"
	"termail/internal/i18n"
	"termail/internal/toast"
)

// useKey marks a key as handled: the hover highlight gives way to the
// keyboard, and the next frame is drawn.
func (m *Model) useKey(cmd tea.Cmd) tea.Cmd {
	m.hoverOn = false
	return cmd
}

// skipKey is for a key that does nothing: keep the pointer, skip the redraw.
func (m *Model) skipKey() tea.Cmd {
	m.noRender = true
	return nil
}

func (m *Model) finishKey(cmd tea.Cmd, ok bool) tea.Cmd {
	if !ok {
		return m.skipKey()
	}
	return m.useKey(cmd)
}

func (m *Model) onKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if m.login != nil {
		if key == "ctrl+c" && m.login.busy {
			return m.useKey(m.exitNow())
		}
		return m.useKey(m.login.update(k)) // in a field Ctrl+C copies (never quits)
	}
	if m.newFolder != nil {
		return m.useKey(m.newFolderKey(k))
	}
	if m.ctxMenu != nil {
		return m.useKey(m.ctxMenu.key(m, key))
	}
	if m.bodyFind != nil && (m.bodyFind.in.Focused() || key == "esc" || key == "enter" || key == "shift+enter") {
		if key == "ctrl+c" && m.copySelection() {
			return m.useKey(nil)
		}
		return m.useKey(m.bodyFindKey(k))
	}
	if key == "ctrl+c" && !m.inTextField() {
		// outside text fields: copy the selected mail text, otherwise quit
		if m.copySelection() {
			return m.useKey(nil)
		}
		return m.useKey(m.exitNow())
	}
	if m.confirm != nil {
		return m.useKey(m.confirm.key(m, key))
	}
	if m.picker != nil {
		return m.useKey(m.picker.key(m, key))
	}
	if m.links != nil {
		return m.useKey(m.links.key(m, key))
	}
	// freeScroll is cleared only when a handled list/folder/reader key
	// should bring the cursor row into view — not on unhandled keys,
	// which would snap the list (cursor flicker) and do nothing else.
	if m.blocks != nil {
		return m.useKey(m.blocks.key(m, key))
	}
	if m.acctPicker != nil {
		return m.useKey(m.acctPicker.key(m, key))
	}
	if m.help {
		if key == "l" || key == "L" || key == "shift+l" {
			m.toggleLang()
			return m.useKey(nil)
		}
		if key == "?" || key == "esc" || key == "q" {
			m.help = false
			return m.useKey(nil)
		}
		return m.skipKey()
	}
	// tab switching works everywhere
	if strings.HasPrefix(key, "alt+") && len(key) == 5 && key[4] >= '1' && key[4] <= '9' {
		m.switchTab(int(key[4] - '1'))
		return m.useKey(nil)
	}
	switch key {
	case "ctrl+w":
		return m.useKey(m.closeTab(m.active))
	case "ctrl+b":
		if m.tabs[m.active].kind != tabCompose {
			m.blocks = &blockScreen{}
			return m.useKey(nil)
		}
	}
	t := m.tabs[m.active]
	if t.kind == tabCompose {
		return m.useKey(t.compose.key(m, k))
	}
	if m.search != nil && m.search.focused {
		return m.useKey(m.search.key(m, k))
	}
	key = navKey(k, key)
	// attachment mode: numbers pick attachments, not accounts
	if r := m.activeReader(); r != nil && r.attMode {
		if cmd, ok := r.attModeKey(m, key); ok {
			return m.useKey(cmd)
		}
	}
	// account switching: only here, i.e. never while typing in a text field
	if key == "ctrl+a" {
		m.acctPicker = newAccountPicker(m)
		return m.useKey(nil)
	}
	if i, ok := accountKey(key); ok {
		if i < 0 {
			return m.useKey(m.showUnified())
		}
		if i < len(m.accts) {
			return m.useKey(m.switchAccount(i))
		}
		// F5 is a common refresh key; with fewer than 4 accounts it would
		// otherwise be swallowed and do nothing (the 3800-mail inbox
		// stayed on the first page after F5).
		if key == "f5" {
			return m.useKey(m.refresh())
		}
		return m.skipKey()
	}
	// new-mail toast keys: only while such a toast is visible
	if m.toasts.HasMail() {
		switch key {
		case "x":
			m.toasts.DismissMail()
			return m.useKey(nil)
		case "g":
			return m.useKey(m.jumpToNewMail())
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			n := int(key[0] - '0')
			mails := m.toasts.Mails()
			if n >= 1 && n <= len(mails) {
				return m.useKey(m.jumpToMail(mails[n-1]))
			}
		case "esc":
			if t.kind == tabList && m.focus != focusReader {
				m.toasts.DismissMail()
				return m.useKey(nil)
			}
		}
	}
	switch key {
	case "n":
		return m.useKey(m.openCompose(composeNew, nil, backend.Envelope{}))
	case "alt+n", "alt+N":
		return m.useKey(m.startNewFolder())
	case "?":
		m.help = true
		return m.useKey(nil)
	case "+":
		m.login = newLogin(m, true)
		m.login.resize(m.w)
		return m.useKey(nil)
	}
	if key == "/" || key == "ctrl+f" {
		if t.kind == tabRead || (t.kind == tabList && m.focus == focusReader && m.pv != nil) {
			return m.useKey(m.openBodyFind())
		}
		if t.kind == tabList {
			return m.useKey(m.openSearch())
		}
	}
	if t.kind == tabRead {
		return m.finishKey(t.read.handleKey(m, key))
	}
	if m.focus == focusFolders {
		return m.finishKey(m.folderKey(key))
	}
	if m.focus == focusReader && m.pv != nil {
		return m.finishKey(m.pv.handleKey(m, key))
	}
	return m.finishKey(m.listKey(key))
}

// inTextField: the keyboard is typing into a text field right now.
// navKey maps Windows / conhost arrow events onto the plain names the
// handlers use. Arrow keys that arrive with an Alt bit set (common when
// a previous AltGr or menu key left the modifier down) would otherwise
// be "alt+right" and do nothing.
func asKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+e":
		return tea.KeyMsg{Type: tea.KeyCtrlE}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case "ctrl+l":
		return tea.KeyMsg{Type: tea.KeyCtrlL}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	case "f2":
		return tea.KeyMsg{Type: tea.KeyF2}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "shift+enter":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shift+enter")}
	case "alt+n", "alt+N":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}, Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func navKey(k tea.KeyMsg, key string) string {
	switch k.Type {
	case tea.KeyRight:
		return "right"
	case tea.KeyLeft:
		return "left"
	case tea.KeyUp:
		return "up"
	case tea.KeyDown:
		return "down"
	}
	return key
}

func (m *Model) inTextField() bool {
	t := m.tabs[m.active]
	if t.kind == tabCompose {
		return true
	}
	if m.newFolder != nil {
		return true
	}
	if m.bodyFind != nil && m.bodyFind.in.Focused() {
		return true
	}
	return m.search != nil && m.search.focused && m.confirm == nil && m.picker == nil && m.links == nil && m.blocks == nil && m.acctPicker == nil && !m.help
}

func (m *Model) switchTab(i int) {
	if i >= 0 && i < len(m.tabs) {
		m.active = i
	}
}

func (m *Model) closeTab(i int) tea.Cmd {
	if i <= 0 || i >= len(m.tabs) {
		return nil
	}
	t := m.tabs[i]
	if t.compose != nil && t.compose.dirty() && !t.compose.closing {
		// Ctrl+W / ✕ on a compose tab asks whether to keep a draft.
		return t.compose.askClose(m)
	}
	m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
	if m.active >= i {
		m.active--
	}
	if m.active < 0 {
		m.active = 0
	}
	return nil
}

func (m *Model) removeTab(t *tab) {
	for i, x := range m.tabs {
		if x == t {
			m.tabs[i].compose = nil
			m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
			if m.active >= i {
				m.active = max(0, m.active-1)
			}
			return
		}
	}
}

func (m *Model) listKey(key string) (tea.Cmd, bool) {
	if cmd, ok := m.groupKey(key); ok {
		m.freeScroll = false
		return cmd, true
	}
	v := m.visible()
	e, has := m.selected()
	ai, fo := m.loc(e)
	move := func(d int) tea.Cmd {
		m.cursor += d
		m.clampCursor()
		return m.previewCmd()
	}
	used := func(cmd tea.Cmd) (tea.Cmd, bool) {
		m.freeScroll = false
		return cmd, true
	}
	switch key {
	case "q":
		return used(m.quit())
	case "D": // Shift+D: empty trash/spam, or move every mail in this folder/view to the trash
		return used(m.shiftD())
	case "up":
		return used(move(-1))
	case "down":
		return used(move(1))
	case "pgup":
		return used(move(-10))
	case "pgdown":
		return used(move(10))
	case "home":
		m.cursor = 0
		return used(m.previewCmd())
	case "end":
		m.cursor = len(v) - 1
		m.clampCursor()
		cmd := m.previewCmd()
		if m.listIncomplete(m.cur, m.folder) {
			return used(tea.Batch(cmd, m.continueListLoad(m.cur, m.folder)))
		}
		return used(cmd)
	case "left", "h":
		m.focus = focusFolders
		m.syncFolderCursor()
		return used(nil)
	case "tab":
		m.switchTab((m.active + 1) % len(m.tabs))
		return used(nil)
	case "shift+tab":
		m.switchTab((m.active - 1 + len(m.tabs)) % len(m.tabs))
		return used(nil)
	case "/", "ctrl+f":
		return used(m.openSearch())
	case "G": // Shift+G: group by sender / one line per mail
		return used(m.toggleGrouping())
	case "esc":
		if m.clearSel() {
			return used(nil)
		}
		if m.search != nil {
			return used(m.closeSearch())
		}
		return used(nil)
	case "ctrl+r", "f5":
		return used(m.refresh())
	}
	if !has {
		if key == "u" { // e.g. after Shift+D emptied the list
			return used(m.undoLast())
		}
		return nil, false
	}
	switch key {
	case "enter", "right":
		if l := m.layout(); l.rw == 0 && !l.readerBelow {
			return used(m.openReader(ai, fo, e)) // no reading pane on a narrow window
		}
		return used(m.enterReader(e))
	}
	if cmd, ok := m.mailAction(key, ai, fo, e); ok {
		return used(cmd)
	}
	if key == "u" {
		return used(m.undoLast())
	}
	return nil, false
}

// mailAction runs a list-style command on a captured envelope. Used from
// the list and from other panes (folder / reader) so o, t, d, … act on
// the selected or previewed mail wherever the keyboard is.
func (m *Model) mailAction(key string, ai int, fo string, e backend.Envelope) (tea.Cmd, bool) {
	switch key {
	case "o":
		return m.openInBrowser(ai, fo, e), true
	case "t":
		return m.openReader(ai, fo, e), true
	case attKey, "e": // open the mail at its attachments (never archive)
		var cmd tea.Cmd
		if l := m.layout(); l.rw == 0 && !l.readerBelow {
			cmd = m.openReader(ai, fo, e)
		} else {
			cmd = m.enterReader(e)
		}
		if r := m.activeReader(); r != nil {
			r.gotoAttachments(m)
		}
		return cmd, true
	case "r":
		return m.openCompose(composeReply, m.bodyOf(e), e), true
	case "a", "R":
		return m.openCompose(composeReplyAll, m.bodyOf(e), e), true
	case "f":
		return m.openCompose(composeForward, m.bodyOf(e), e), true
	case "d", "delete":
		return m.deleteMsg(ai, fo, e), true
	case "u":
		return m.undoLast(), true
	case "i":
		on := !e.Seen
		if m.stickySec == nil {
			m.stickySec = map[foldKey]bool{}
		}
		k := foldKey{ai, fo, e.UID}
		if _, ok := m.stickySec[k]; !ok {
			m.stickySec[k] = m.inUnreadList(ai, fo, e) // stays under the cursor until leaving / refresh
		}
		m.setSeenLocal(ai, fo, e.UID, on)
		if on {
			m.toasts.MarkRead(ai, fo, e.UID)
		}
		e.Seen = on
		return m.queueFlagUndo(ai, fo, e, backend.FlagSeen, on), true
	case "k":
		return m.toggleSaved(ai, fo, e), true
	case "m":
		m.picker = newFolderPicker(m, ai, fo, e)
		return nil, true
	case linksKey:
		return m.openLinks(m.body(bodyKey{ai, fo, e.UID})), true
	case "c":
		return m.copyBody(bodyKey{ai, fo, e.UID}, false, 0), true
	case "C":
		return m.copySender(e, m.body(bodyKey{ai, fo, e.UID})), true
	case "b":
		return m.blockSender(ai, fo, e, blocklist.Address), true
	case "B":
		return m.blockSender(ai, fo, e, blocklist.Domain), true
	}
	return nil, false
}

// listWideAction runs list commands that are not tied to folder navigation
// (Shift+G, and mail actions) when the keyboard is in another pane.
func (m *Model) listWideAction(key string, ai int, fo string, e backend.Envelope, hasMail, allowArchive bool) (tea.Cmd, bool) {
	if key == "G" {
		m.focus = focusList
		if m.active >= 0 && m.active < len(m.tabs) && m.tabs[m.active].kind == tabRead {
			m.active = 0
		}
		m.freeScroll = false
		return m.toggleGrouping(), true
	}
	if key == "D" {
		m.focus = focusList
		m.freeScroll = false
		return m.shiftD(), true
	}
	if !hasMail {
		if key == "u" {
			return m.undoLast(), true
		}
		return nil, false
	}
	cmd, ok := m.mailAction(key, ai, fo, e)
	if ok {
		m.freeScroll = false
	}
	return cmd, ok
}

func (m *Model) listWideFromFolders(key string) (tea.Cmd, bool) {
	e, has := m.selected()
	ai, fo := m.loc(e)
	cmd, ok := m.listWideAction(key, ai, fo, e, has, true)
	if ok && key != "G" && key != "D" && has {
		m.focus = focusList
	}
	return cmd, ok
}

func (m *Model) bodyOf(e backend.Envelope) *bodyRef {
	ai, fo := m.loc(e)
	return &bodyRef{key: bodyKey{ai, fo, e.UID}}
}

// ---- folder panel ----

func (m *Model) folderRowsSelectable() []frow {
	var out []frow
	for _, r := range m.folderRows() {
		if r.kind == rowAcct || r.kind == rowFolder || r.kind == rowUnified || r.kind == rowSaved {
			out = append(out, r)
		}
	}
	out = append(out, frow{kind: rowBlocked}, frow{kind: rowAddAcct})
	return out
}

func (m *Model) syncFolderCursor() {
	for i, r := range m.folderRowsSelectable() {
		if m.virt == viewUnified && r.kind == rowUnified && r.read == m.readView || m.virt == viewSaved && r.kind == rowSaved ||
			m.virt == viewFolder && r.kind == rowFolder && r.acct == m.cur && r.folder == m.folder && r.read == m.readView {
			m.fcursor = i
			return
		}
	}
}

func (m *Model) folderKey(key string) (tea.Cmd, bool) {
	rows := m.folderRowsSelectable()
	if m.fcursor >= len(rows) {
		m.fcursor = len(rows) - 1
	}
	if m.fcursor < 0 {
		m.fcursor = 0
	}
	switch key {
	case "q":
		return m.quit(), true
	case "alt+n", "alt+N":
		return m.startNewFolder(), true
	case "D":
		return m.folderShiftD(), true
	case "d":
		if r, ok := m.customFolderAtCursor(); ok {
			return m.askDeleteFolder(r), true
		}
	case "r":
		if r, ok := m.customFolderAtCursor(); ok {
			return m.startRenameFolder(r), true
		}
	case "up":
		if m.fcursor > 0 {
			m.fcursor--
			return m.folderPreview(), true
		}
		return nil, true
	case "down":
		if m.fcursor < len(rows)-1 {
			m.fcursor++
			return m.folderPreview(), true
		}
		return nil, true
	case "right", "l":
		if r := rows[m.fcursor]; r.kind == rowAcct {
			if !m.accts[r.acct].Open {
				m.setOpen(r.acct, true) // expand
			} else if m.fcursor+1 < len(rows) && rows[m.fcursor+1].kind == rowFolder && rows[m.fcursor+1].acct == r.acct {
				m.fcursor++ // already open: step to its first folder
				return m.folderPreview(), true
			}
			return nil, true
		}
		m.focus = focusList
		return tea.Batch(m.commitPending(), m.previewCmd()), true
	case "esc":
		m.focus = focusList
		return tea.Batch(m.commitPending(), m.previewCmd()), true
	case "left", "h":
		r := rows[m.fcursor]
		switch r.kind {
		case rowAcct:
			m.setOpen(r.acct, false)
		case rowFolder: // up to the account row
			for i := m.fcursor - 1; i >= 0; i-- {
				if rows[i].kind == rowAcct && rows[i].acct == r.acct {
					m.fcursor = i
					break
				}
			}
		}
		return nil, true
	case "tab":
		m.switchTab((m.active + 1) % len(m.tabs))
		return nil, true
	case " ", "space":
		if r := rows[m.fcursor]; r.kind == rowAcct {
			m.setOpen(r.acct, !m.accts[r.acct].Open)
			return nil, true
		}
	case "delete":
		if r := rows[m.fcursor]; r.kind == rowAcct {
			m.confirmRemoveAccount(r.acct)
			return nil, true
		}
	case "enter":
		r := rows[m.fcursor]
		switch r.kind {
		case rowAcct:
			m.setOpen(r.acct, !m.accts[r.acct].Open)
			return nil, true
		case rowFolder:
			if m.virt == viewFolder && m.cur == r.acct && m.folder == r.folder && m.readView == r.read && m.pendingF == nil {
				m.focus = focusList // already previewed: just step into the list
				return m.previewCmd(), true
			}
			m.pendingF = nil
			return m.openFolderView(r.acct, r.folder, r.read), true
		case rowUnified:
			if r.read {
				return m.showVirtualView(viewUnified, true), true
			}
			return m.showUnified(), true
		case rowSaved:
			return m.showSaved(), true
		case rowBlocked:
			m.blocks = &blockScreen{}
			return nil, true
		case rowAddAcct:
			m.login = newLogin(m, true)
			m.login.resize(m.w)
			return nil, true
		}
		return nil, true
	}
	return m.listWideFromFolders(key)
}

// shiftD runs Shift+D on the folder or view currently shown in the list:
// trash/spam are emptied permanently; every other folder (inbox, drafts,
// sent, custom, unified, saved) moves all of its mail to the trash.
func (m *Model) shiftD() tea.Cmd {
	if m.virt == viewSaved {
		return nil
	}
	if m.virt == viewFolder {
		if a := m.acct(); a != nil && emptyable(a.folder(m.folder)) {
			return m.askEmpty()
		}
	}
	return m.askBulkDelete()
}

// folderShiftD runs Shift+D on the folder under the left-pane cursor,
// without requiring the list pane to be focused first.
func (m *Model) folderShiftD() tea.Cmd {
	r, ok := m.folderCursorRow()
	if !ok {
		return nil
	}
	focus := focusFolders
	switch r.kind {
	case rowUnified:
		if r.read {
			return nil
		}
		cmd := m.showVirtualView(viewUnified, false)
		m.focus = focus
		return tea.Batch(cmd, m.askBulkDelete())
	case rowFolder:
		if r.acct < 0 || r.acct >= len(m.accts) {
			return nil
		}
		m.pendingF = nil
		m.switchFolderView(r.acct, r.folder, r.read)
		m.focus = focus
		f := m.accts[r.acct].folder(r.folder)
		if emptyable(f) {
			return m.askEmpty()
		}
		return m.askBulkDelete()
	default:
		return nil
	}
}

func (m *Model) folderCursorRow() (frow, bool) {
	rows := m.folderRowsSelectable()
	if m.fcursor < 0 || m.fcursor >= len(rows) {
		return frow{}, false
	}
	return rows[m.fcursor], true
}

func (m *Model) openFolder(ai int, folder string) tea.Cmd {
	return m.openFolderView(ai, folder, false)
}

// openRead opens an account's Okunanlar (its inbox, read mails only).
func (m *Model) openRead(ai int) tea.Cmd {
	return m.openFolderView(ai, m.accts[ai].inbox(), true)
}

func (m *Model) openFolderView(ai int, folder string, read bool) tea.Cmd {
	m.virt = viewFolder
	m.cur, m.folder = ai, folder
	m.readView = read
	m.cursor, m.offset = 0, 0
	m.stickySec, m.opened = nil, nil
	m.dropSearch()
	m.focus = focusList
	return tea.Batch(m.loadList(ai, folder, false), m.previewCmd())
}

// jumpToNewMail opens the newest mail of the new-mail toast.
func (m *Model) jumpToNewMail() tea.Cmd {
	t, ok := m.toasts.Target()
	if !ok {
		return nil
	}
	return m.jumpToMail(t)
}

// jumpToMail opens one new-mail toast entry (clicked or chosen by number).
func (m *Model) jumpToMail(t toast.Mail) tea.Cmd {
	if t.Account >= len(m.accts) {
		return nil
	}
	a := m.accts[t.Account]
	a.Open = true
	if !m.showing(t.Account, t.Folder) || m.readView {
		m.readView = false // new mail is unread: Gelen Kutusu
		m.virt = viewFolder
		m.cur, m.folder = t.Account, t.Folder
		m.dropSearch()
	}
	m.focus = focusList
	m.active = 0
	{ // the new mail may sit in a closed sender group or section
		for _, e := range m.baseList() {
			if ai, _ := m.loc(e); e.UID == t.UID && ai == t.Account {
				m.revealMail(e)
				break
			}
		}
	}
	for i, e := range m.visible() {
		if ai, _ := m.loc(e); !isGroupRow(e) && e.UID == t.UID && ai == t.Account {
			m.cursor = i
			return m.openReader(t.Account, t.Folder, e)
		}
	}
	m.toasts.MarkRead(t.Account, t.Folder, t.UID)
	return m.loadList(t.Account, t.Folder, false)
}

func (m *Model) confirmRemoveAccount(ai int) {
	a := m.accts[ai]
	m.confirm = &confirmBox{title: i18n.T("Hesabı kaldır"), text: a.Cfg.Name + " (" + a.Cfg.Email + i18n.T(") bu bilgisayardan kaldırılsın mı? Postaların sunucuda kalır."),
		yes: i18n.T("Kaldır"), onYes: func() tea.Cmd {
			if a.cancel != nil {
				a.cancel()
			}
			if a.B != nil {
				go a.B.Close()
			}
			if m.opt.Config != nil && !m.opt.Demo {
				removeAccountConfig(m.opt.Config, a.Cfg.Email) // config + saved sign-in
			} else if !m.opt.Demo {
				deleteAccountSecrets(a.Cfg.Email)
			}
			m.accts = append(m.accts[:ai], m.accts[ai+1:]...)
			m.tabs = m.tabs[:1]
			m.active, m.cur, m.cursor = 0, 0, 0
			m.toasts.DismissMail()
			if len(m.accts) == 0 {
				m.login = newLogin(m, false)
				m.login.resize(m.w)
				return nil
			}
			m.folder = m.accts[0].inbox()
			m.fcursor = 0
			return nil
		}}
}

var _ = toast.Info

// refresh reloads what is on screen (every account for the merged views).
func (m *Model) refresh() tea.Cmd {
	m.stickySec = nil // mails toggled with i now go to their list
	switch m.virt {
	case viewUnified:
		var cmds []tea.Cmd
		for i, a := range m.accts {
			cmds = append(cmds, m.kickOrReloadList(i, a.inbox()), m.loadFolders(i))
		}
		return tea.Batch(cmds...)
	case viewSaved:
		var cmds []tea.Cmd
		for i := range m.accts {
			cmds = append(cmds, m.loadSaved(i))
		}
		return tea.Batch(cmds...)
	}
	return tea.Batch(m.kickOrReloadList(m.cur, m.folder), m.loadFolders(m.cur))
}

// toggleLang switches the UI language and writes it to config.json.
func (m *Model) toggleLang() {
	next := i18n.EN
	if i18n.Get() == i18n.EN {
		next = i18n.TR
	}
	i18n.Set(next)
	if m.opt.Config != nil {
		m.opt.Config.Language = string(next)
	}
	if err := account.SaveLanguage(string(next)); err != nil && m.opt.Logger != nil {
		m.opt.Logger.Println("language:", err)
	}
}
