package ui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/blocklist"
	"termail/internal/downloads"
	"termail/internal/grid"
	"termail/internal/i18n"
	"termail/internal/mailmsg"
	"termail/internal/toast"
)

type reader struct {
	key      bodyKey
	env      backend.Envelope
	scroll   int
	att      int  // selected attachment
	showAtt  bool // showing a text attachment instead of the body
	attMode  bool // the "Ekler ve Bağlantılar" tab is shown (e / Shift+E / click)
	sel      int  // selected row on that tab (attachments first, then links)
	tabTop   int  // its scroll position (rows)
	prev     []prevLine
	prevAtt  int // the attachment / size the cached preview is for
	prevW    int
	prevH    int
	unread   bool
	lastH    int
	inline   bool      // the reading pane beside the list, not a tab of its own
	fromRead bool      // opened from Okunanlar (next/prev stay in that list)
	hist     []bodyKey // previously shown mails, for [
}

// leave closes a reader tab or returns focus from the reading pane to the list.
func (r *reader) leave(m *Model) {
	if r.inline {
		m.focus = focusList
		return
	}
	m.removeTab(m.tabs[m.active])
}

func (m *Model) openReader(ai int, folder string, e backend.Envelope) tea.Cmd {
	k := bodyKey{ai, folder, e.UID}
	for i, t := range m.tabs {
		if t.read != nil && t.read.key == k {
			m.active = i
			return m.markRead(ai, folder, e.UID)
		}
	}
	r := &reader{key: k, env: e, fromRead: m.readView}
	m.tabs = append(m.tabs, &tab{kind: tabRead, title: trimSubj(e.Subject), read: r})
	m.active = len(m.tabs) - 1
	e.Seen = true
	r.env = e
	cmd := m.markRead(ai, folder, e.UID)
	m.followOpened(e, false) // it moved to Okunanlar
	return tea.Batch(m.loadBody(k), cmd)
}

func (r *reader) handleKey(m *Model, key string) (tea.Cmd, bool) {
	p := m.body(r.key)
	e := r.env
	switch key {
	case "esc", "q":
		if key == "esc" && m.clearSel() {
			return nil, true
		}
		if r.inline {
			m.focus = focusList
			return m.leaveOpened(), true
		}
		m.active = 0
		return nil, true
	case "]":
		return m.stepReader(r, 1), true
	case "[":
		return m.stepReader(r, -1), true
	case "enter":
		// Enter inside an opened mail answers it (in a new tab).
		return m.openCompose(composeReply, &bodyRef{key: r.key}, e), true
	case "o":
		return m.openInBrowser(r.key.acct, r.key.folder, e), true
	case "c":
		return m.copyBody(r.key, r.showAtt, r.att), true
	case "C":
		return m.copySender(e, p), true
	case "tab":
		m.switchTab((m.active + 1) % len(m.tabs))
		return nil, true
	case "shift+tab":
		m.switchTab((m.active - 1 + len(m.tabs)) % len(m.tabs))
		return nil, true
	case "up":
		r.scroll = max(0, r.scroll-1)
		return nil, true
	case "down":
		r.scroll++
		return nil, true
	case "pgup":
		r.scroll = max(0, r.scroll-max(1, r.lastH-2))
		return nil, true
	case "pgdown", " ", "space":
		r.scroll += max(1, r.lastH-2)
		return nil, true
	case "home":
		r.scroll = 0
		return nil, true
	case "end":
		r.scroll = 1 << 30
		return nil, true
	case "left", "h":
		if r.inline {
			m.focus = focusList
			return m.leaveOpened(), true
		}
		m.active = 0 // reader tab: step back to the list
		return nil, true
	case linksKey:
		return m.openLinks(p), true
	case attKey, "e":
		r.gotoAttachments(m)
		return nil, true
	case "s":
		return r.saveAttachment(m), true
	case "k":
		cmd := m.toggleSaved(r.key.acct, r.key.folder, e)
		r.env.Flagged = !e.Flagged
		return cmd, true
	case "v":
		r.gotoAttachments(m)
		return nil, true
	case "r":
		return m.openCompose(composeReply, &bodyRef{key: r.key}, e), true
	case "a", "R":
		return m.openCompose(composeReplyAll, &bodyRef{key: r.key}, e), true
	case "f":
		return m.openCompose(composeForward, &bodyRef{key: r.key}, e), true
	case "d", "delete":
		cmd := m.deleteMsg(r.key.acct, r.key.folder, e)
		if m.confirm == nil {
			r.leave(m)
		}
		return cmd, true
	case "u":
		return m.undoLast(), true
	case "b":
		cmd := m.blockSender(r.key.acct, r.key.folder, e, blocklist.Address)
		r.leave(m)
		return cmd, true
	case "B":
		cmd := m.blockSender(r.key.acct, r.key.folder, e, blocklist.Domain)
		r.leave(m)
		return cmd, true
	}
	// List actions this pane does not bind (t, Shift+G, i, m, …) still
	// apply to the open mail / the list, so they are not swallowed.
	if cmd, ok := m.listWideAction(key, r.key.acct, r.key.folder, e, e.UID != 0, false); ok {
		return cmd, true
	}
	return nil, false
}

func (r *reader) saveAttachment(m *Model) tea.Cmd {
	p := m.body(r.key)
	if p == nil {
		m.toastInfo(toast.Info, "", i18n.T("İleti henüz yükleniyor."))
		return nil
	}
	if len(p.Attachments) == 0 {
		m.toastInfo(toast.Info, "", "Bu iletide ek yok.")
		return nil
	}
	a := p.Attachments[min(r.att, len(p.Attachments)-1)]
	return func() tea.Msg {
		dir, err := downloads.Dir()
		if err != nil {
			return savedMsg{err: err}
		}
		path, err := downloads.Save(dir, a.Name, a.Data)
		return savedMsg{path: path, err: err}
	}
}

func (r *reader) draw(m *Model, g *grid.Grid) {
	top := 2
	h := g.H - 3
	g.Box(0, top, g.W, h, cBorderF, cBG, i18n.T("İleti · ")+trimSubj(r.env.Subject), cAccent)
	m.hits.add(hit{x: 0, y: top, w: g.W, h: h, kind: hitReaderPane})
	x, w := 3, g.W-6
	p := m.body(r.key)
	attSel := -1
	if p != nil && len(p.Attachments) > 0 {
		attSel = r.att
	}
	var hl []string
	if m.searching() {
		hl = m.search.q.Needles()
	}
	m.drawReaderTabs(g, x, top+1, w, r, p)
	g.HLine(0, top+2, g.W, cBorder, "├", "┤")
	if r.attMode {
		m.drawAttTab(g, x+1, top+4, min(w-1, 100), top+h-2, r, p, true)
		keybar(g, g.H-1, readerKeys(p, false, true), i18n.T("? Yardım"), &m.hits)
		return
	}
	y := drawHeaders(g, x, top+4, w, r.env, p, attSel, true, hl, false, func(i, ay int) {
		m.hits.add(hit{x: x, y: ay, w: w, h: 1, kind: hitAttachment, idx: i})
	})
	g.HLine(0, y, g.W, cBorder, "├", "┤")
	if r.showAtt && p != nil {
		g.Put(x, y, " Ek: "+p.Attachments[r.att].Name+" ", stb(cPurple), 0)
	}
	y += 2
	bottom := top + h - 2
	r.lastH = bottom - y
	bodyW := min(w, 100)
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
		lines, paras := m.wrapped(r.key, text, bodyW)
		if r.scroll > max(0, len(lines)-r.lastH) {
			r.scroll = max(0, len(lines)-r.lastH)
		}
		m.tv = textView{ok: true, x: x + 1, y: y, w: bodyW, h: max(1, r.lastH), lines: lines, paras: paras, r: r, key: r.key, att: r.showAtt}
		for i := r.scroll; i < len(lines) && y < bottom; i++ {
			fg := cFG
			if strings.HasPrefix(lines[i].text, ">") {
				fg = cFaint
			}
			m.drawBodyLine(g, x+1, y, lines[i].text, fg, bodyW, links)
			if len(hl) > 0 {
				hlCells(g, x+1, y, lines[i].text, hl, bodyCellW)
			}
			y++
		}
		m.paintSel(g)
		m.paintBodyFind(g)
		if p.FromHTML {
			g.Put(x, top+h-2, i18n.T("Bu ileti sade metin olarak gösteriliyor; görseller yüklenmedi."), st(cFaint), w)
		}
		if len(lines) > r.lastH {
			pct := 100
			if len(lines)-r.lastH > 0 {
				pct = min(100, r.scroll*100/(len(lines)-r.lastH))
			}
			g.RPut(g.W-3, top+h-2, strconv.Itoa(pct)+"%", st(cFaint))
		}
	case m.bodyErr[r.key] != "":
		g.Put(x+1, y, m.bodyErr[r.key], st(cRed), w)
	default:
		g.Put(x+1, y, i18n.T("Yükleniyor…"), st(cFaint), w)
	}
	if m.bodyFind != nil {
		m.drawBodyFind(g, x, top+h-2, w)
	}
	keybar(g, g.H-1, m.withSelKeys(withComposeHint(readerKeys(p, false, r.attMode))), i18n.T("? Yardım"), &m.hits)
}

var _ = mailmsg.Display

// readerKeys is the short key bar while a mail is open (pane or tab).
// The full list lives in the "?" help overlay. Enter still replies;
// o still opens the mail in the browser (next/prev do not apply there).
func readerKeys(p *mailmsg.Parsed, inline bool, attMode bool) []keyHint {
	_ = p
	_ = inline
	if attMode {
		return []keyHint{{"Enter", i18n.T("Aç")}, {"s", i18n.T("Kaydet")}, {"Esc", i18n.T("Mail")}}
	}
	return []keyHint{
		{"r", i18n.T("Yanıtla")}, {"a", i18n.T("Tümüne")}, {"f", i18n.T("İlet")},
		{"e", i18n.T("Ekler")}, {"m", i18n.T("Klasöre taşı")}, {"]", "Sonraki"}, {"[", i18n.T("Önceki")}, {"Esc", i18n.T("Geri")},
	}
}

// attKey jumps to the attachments of the open mail ("Ekler").
const attKey = "E"

// gotoAttachments opens the "Ekler ve Bağlantılar" tab with its first row
// selected (when the mail is still loading, the tab is ready when it comes).
func (r *reader) gotoAttachments(m *Model) {
	if m.body(r.key) == nil {
		r.attMode, r.sel, r.tabTop, r.att, r.showAtt = true, 0, 0, 0, false
		return
	}
	r.showAttTab(m)
}

// activeReader: the reader that has the keyboard (a reader tab or the
// focused reading pane), or nil.
func (m *Model) activeReader() *reader {
	if t := m.tabs[m.active]; t.read != nil {
		return t.read
	}
	if m.focus == focusReader {
		return m.pv
	}
	return nil
}

// followOpened runs after a mail is opened (and so marked read). In Gelen
// Kutusu it moves to Okunanlar: opened in the reading pane it stays until the
// pane is left; opened in a tab it leaves now and the cursor stays at the
// same place, which is the next unread mail.
func (m *Model) followOpened(e backend.Envelope, inline bool) {
	ai, fo := m.loc(e)
	k := foldKey{ai, fo, e.UID}
	if m.stickySec != nil {
		delete(m.stickySec, k) // opening it decides its place
	}
	m.visMemo = nil
	m.opened = nil
	if inline {
		m.opened = &k
	}
	if !m.revealMail(e) {
		m.clampCursor()
	}
}

// leaveOpened: the reading pane gave the keyboard back to the list. The
// mail opened from Gelen Kutusu now leaves it; the cursor stays at its
// place (the next unread mail) and the pane shows that one.
func (m *Model) leaveOpened() tea.Cmd {
	if m.opened == nil {
		return nil
	}
	m.opened = nil
	m.visMemo = nil
	m.clampCursor()
	return m.previewCmd()
}

// stepReader opens the next (delta > 0) or previous mail in the same list
// without returning to the list first. Opening it marks it read like Enter.
func (m *Model) stepReader(r *reader, delta int) tea.Cmd {
	if r == nil || delta == 0 {
		return nil
	}
	list := m.stepList(r)
	idx := -1
	for i, e := range list {
		ai, fo := m.loc(e)
		if e.UID == r.key.uid && ai == r.key.acct && fo == r.key.folder {
			idx = i
			break
		}
	}
	if delta < 0 && len(r.hist) > 0 {
		k := r.hist[len(r.hist)-1]
		r.hist = r.hist[:len(r.hist)-1]
		e, ok := m.envelopeOf(k)
		if !ok {
			m.toastInfo(toast.Info, "", i18n.T("Başka ileti yok."))
			return nil
		}
		return m.openStepped(r, e, false)
	}
	j := idx + delta
	if idx < 0 {
		if delta > 0 {
			j = 0
		} else {
			j = len(list) - 1
		}
	}
	if j < 0 || j >= len(list) {
		m.toastInfo(toast.Info, "", i18n.T("Başka ileti yok."))
		return nil
	}
	e := list[j]
	if delta > 0 {
		r.hist = append(r.hist, r.key)
	}
	return m.openStepped(r, e, true)
}

func (m *Model) envelopeOf(k bodyKey) (backend.Envelope, bool) {
	if k.acct < 0 || k.acct >= len(m.accts) {
		return backend.Envelope{}, false
	}
	for _, e := range m.accts[k.acct].Lists[k.folder] {
		if e.UID == k.uid {
			e.Acct, e.Folder = k.acct, k.folder
			return e, true
		}
	}
	return backend.Envelope{}, false
}

func (m *Model) openStepped(r *reader, e backend.Envelope, remember bool) tea.Cmd {
	ai, fo := m.loc(e)
	_ = remember
	if r.inline {
		m.revealMail(e)
		return m.enterReader(e)
	}
	r.key = bodyKey{ai, fo, e.UID}
	r.env = e
	r.scroll, r.att, r.showAtt, r.attMode, r.sel, r.tabTop = 0, 0, false, false, 0, 0
	r.prev = nil
	if t := m.tabs[m.active]; t != nil && t.read == r {
		t.title = trimSubj(e.Subject)
	}
	e.Seen = true
	r.env = e
	cmd := m.markRead(ai, fo, e.UID)
	m.followOpened(e, false)
	return tea.Batch(m.loadBody(r.key), cmd)
}

// stepList is the sequence next/prev walks: the current list view when
// reading in the pane, otherwise the same folder split (Gelen Kutusu or
// Okunanlar) the mail was opened from.
func (m *Model) stepList(r *reader) []backend.Envelope {
	if r.inline {
		var out []backend.Envelope
		for _, e := range m.visible() {
			if !isGroupRow(e) {
				out = append(out, e)
			}
		}
		return out
	}
	saved := m.readView
	m.readView = r.fromRead
	defer func() { m.readView = saved }()
	var src []backend.Envelope
	if m.virt == viewUnified {
		src = m.splitRead(mergeInboxes(m.accts), 0, "")
	} else if r.key.acct >= 0 && r.key.acct < len(m.accts) {
		src = m.splitRead(m.accts[r.key.acct].Lists[r.key.folder], r.key.acct, r.key.folder)
	}
	var out []backend.Envelope
	for _, e := range src {
		if !isGroupRow(e) {
			out = append(out, e)
		}
	}
	return out
}
