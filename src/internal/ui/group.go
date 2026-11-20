package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/grid"
	"termail/internal/i18n"
	"termail/internal/mailmsg"
	"termail/internal/search"
	"termail/internal/toast"
)

// Grouping by sender (Shift+G): mail from the same organisation (by its
// registrable domain: accounts.google.com and google.com are both
// "Google") is folded under one row "▸ Google (5)  ● 3 yeni  13:14".
// A sender with one mail stays a normal row. The list keeps one entry per
// screen row, so a group header is a placeholder row (UID 0) that no mail
// action ever touches; selected() reports no mail for it.

const grpMark = "\x00grp:"

func isGroupRow(e backend.Envelope) bool { return e.UID == 0 && strings.HasPrefix(e.Folder, grpMark) }

func groupKeyOf(e backend.Envelope) string { return strings.TrimPrefix(e.Folder, grpMark) }

type grpInfo struct {
	key, label string
	n, unread  int
	fresh      int // unread mail newer than the last time the group was opened
	newest     time.Time
	open       bool
}

// groupsOn: the list on screen is grouped (search results never are).
func (m *Model) groupsOn() bool { return m.grouped && !m.searching() }

func (m *Model) senderKey(addr string) string {
	if k, ok := m.brandCache[addr]; ok {
		return k
	}
	if m.brandCache == nil || len(m.brandCache) > 5000 {
		m.brandCache = map[string]string{}
	}
	k := search.GroupKey(addr)
	m.brandCache[addr] = k
	return k
}

// groupRows turns a newest-first list into rows: single mails, group
// headers, and the mail of expanded groups (marked in m.grpChild).
func (m *Model) groupRows(l []backend.Envelope) []backend.Envelope {
	m.grps = map[string]*grpInfo{}
	out, child := m.buildGroups(l)
	m.grpChild = child
	return out
}

// buildGroups turns a list into group headers (+ open members) and single
// rows, adding the groups to m.grps. child[i]: row i is inside a group.
func (m *Model) buildGroups(l []backend.Envelope) ([]backend.Envelope, []bool) {
	var order []string
	members := map[string][]backend.Envelope{}
	for _, e := range l {
		k := m.senderKey(e.FromAddr)
		if _, ok := members[k]; !ok {
			order = append(order, k)
		}
		members[k] = append(members[k], e)
	}
	out := make([]backend.Envelope, 0, len(l))
	child := make([]bool, 0, len(l))
	for _, k := range order {
		ms := members[k]
		if len(ms) == 1 {
			out = append(out, ms[0])
			child = append(child, false)
			continue
		}
		gi := &grpInfo{key: k, n: len(ms), newest: ms[0].Date, open: m.grpOpen[k]}
		var names []string
		seen := m.prefs.GroupSeen[k]
		for _, e := range ms {
			if e.Date.After(gi.newest) {
				gi.newest = e.Date
			}
			if !e.Seen {
				gi.unread++
				if e.Date.Unix() > seen {
					gi.fresh++
				}
			}
			names = append(names, tidy(e.From))
		}
		gi.label = search.GroupLabel(k, names)
		m.grps[k] = gi
		out = append(out, backend.Envelope{Folder: grpMark + k, Subject: gi.label, Date: gi.newest, Seen: gi.unread == 0, Acct: ms[0].Acct})
		child = append(child, false)
		if gi.open {
			out = append(out, ms...)
			for range ms {
				child = append(child, true)
			}
		}
	}
	return out, child
}

// cursorGroup: the group header under the cursor, or the group of the
// mail under the cursor (child = true).
func (m *Model) cursorGroup() (gi *grpInfo, hdr int, child bool) {
	if !m.groupsOn() {
		return nil, -1, false
	}
	v := m.visible()
	if m.cursor < 0 || m.cursor >= len(v) {
		return nil, -1, false
	}
	if isGroupRow(v[m.cursor]) {
		return m.grps[groupKeyOf(v[m.cursor])], m.cursor, false
	}
	if m.cursor < len(m.grpChild) && m.grpChild[m.cursor] {
		for i := m.cursor - 1; i >= 0; i-- {
			if isGroupRow(v[i]) {
				return m.grps[groupKeyOf(v[i])], i, true
			}
		}
	}
	return nil, -1, false
}

// setGroupOpen expands / collapses a group; opening it clears its badge.
func (m *Model) setGroupOpen(gi *grpInfo, open bool) {
	if gi == nil {
		return
	}
	if open {
		m.grpOpen[gi.key] = true
		if gi.newest.Unix() > m.prefs.GroupSeen[gi.key] {
			if m.prefs.GroupSeen == nil {
				m.prefs.GroupSeen = map[string]int64{}
			}
			m.prefs.GroupSeen[gi.key] = gi.newest.Unix()
			m.savePrefs()
		}
	} else {
		delete(m.grpOpen, gi.key)
	}
}

// groupKey handles a key on a group header or inside a group; ok = done.
func (m *Model) groupKey(key string) (tea.Cmd, bool) {
	gi, hdr, child := m.cursorGroup()
	if gi == nil {
		return nil, false
	}
	if child {
		if key == "left" || key == "h" {
			m.setGroupOpen(gi, false)
			m.cursor = hdr
			return m.previewCmd(), true
		}
		return nil, false
	}
	switch key {
	case "enter", " ", "space":
		m.setGroupOpen(gi, !gi.open)
		return nil, true
	case "right", "l":
		if !gi.open {
			m.setGroupOpen(gi, true)
			return nil, true
		}
		m.cursor = hdr + 1
		return m.previewCmd(), true
	case "left", "h":
		if gi.open {
			m.setGroupOpen(gi, false)
			return nil, true
		}
		return nil, false // a closed group: ← goes to the folders as usual
	case "d", "delete":
		return m.askDeleteSender(), true
	case "t":
		return m.openGroupTab(gi), true
	case "m":
		if m.inTrashView() {
			return m.restoreSenderMails(), true
		}
		m.toastInfo(toast.Info, "", i18n.T("Bu bir grup başlığı. Önce Enter ile aç."))
		return nil, true
	case "e", "b", "B", "k", "r", "a", "R", "f", "o", "i", "c", "C", linksKey, attKey:
		m.toastInfo(toast.Info, "", i18n.T("Bu bir grup başlığı. Önce Enter ile aç."))
		return nil, true
	}
	return nil, false
}

func (m *Model) openGroupTab(gi *grpInfo) tea.Cmd {
	if gi == nil {
		return nil
	}
	m.setGroupOpen(gi, true)
	v := m.visible()
	for i, e := range v {
		if isGroupRow(e) && groupKeyOf(e) == gi.key {
			if i+1 < len(v) && !isGroupRow(v[i+1]) {
				m.cursor = i + 1
				ai, fo := m.loc(v[i+1])
				return m.openReader(ai, fo, v[i+1])
			}
			break
		}
	}
	return nil
}

// toggleGrouping is Shift+G: grouped by sender <-> one line per mail.
func (m *Model) toggleGrouping() tea.Cmd {
	cur, ok := m.selected()
	m.grouped = !m.grouped
	m.savePrefs()
	m.cursor, m.offset = 0, 0
	if ok && !m.findMail(cur) && m.groupsOn() {
		// the mail sits in a closed group: highlight the group's row
		k := m.senderKey(cur.FromAddr)
		for i, e := range m.visible() {
			if isGroupRow(e) && groupKeyOf(e) == k {
				m.cursor = i
				break
			}
		}
	}
	if m.grouped {
		m.toastInfo(toast.Info, "", i18n.T("Gönderene göre gruplandı."))
	} else {
		m.toastInfo(toast.Info, "", i18n.T("Düz liste."))
	}
	return m.previewCmd()
}

// revealMail puts the cursor on a mail, expanding its group if needed.
func (m *Model) revealMail(e backend.Envelope) bool {
	if m.findMail(e) {
		return true
	}
	if m.groupsOn() {
		m.visible()
		k := m.senderKey(e.FromAddr)
		if gi := m.grps[k]; gi != nil {
			m.setGroupOpen(gi, true)
			return m.findMail(e)
		}
	}
	return false
}

// findMail puts the cursor on a mail that is on screen.
func (m *Model) findMail(e backend.Envelope) bool {
	ai, fo := m.loc(e)
	for i, x := range m.visible() {
		if isGroupRow(x) {
			continue
		}
		if xa, xf := m.loc(x); x.UID == e.UID && xa == ai && xf == fo {
			m.cursor = i
			return true
		}
	}
	return false
}

// drawGroupRow draws "▸ Google (5)      ● 3 yeni  13:14".
func (m *Model) drawGroupRow(g *grid.Grid, x0, w, mx, mw, y int, e backend.Envelope, sel, focused bool) {
	gi := m.grps[groupKeyOf(e)]
	if gi == nil {
		return
	}
	bg := cRaised
	if sel {
		bg = cSelFocus
	}
	g.Fill(x0+1, y, w-2, 1, bg)
	arrow, ac := "▸", cDim
	if gi.open {
		arrow = "▾"
	}
	if sel || gi.open {
		ac = cAccent
	}
	g.Put(mx, y, arrow, stbg(ac, bg), 0)
	date := mailmsg.ShortDate(gi.newest, m.opt.Now())
	right := grid.Width(date)
	badge := ""
	if gi.fresh > 0 && !gi.open {
		badge = i18n.Tf("● %d yeni", gi.fresh)
		right += grid.Width(badge) + 2
	}
	cnt := fmt.Sprintf(" (%d)", gi.n)
	x := g.Put(mx+2, y, gi.label, stbgb(cFG, bg, true), max(4, mw-2-right-grid.Width(cnt)-1))
	g.Put(x, y, cnt, stbg(cFaint, bg), 0)
	g.RPut(mx+mw, y, date, stbg(cDim, bg))
	if badge != "" {
		g.RPut(mx+mw-grid.Width(date)-2, y, badge, stbgb(cYellow, bg, true))
	}
}

// groupPreview fills the reading pane while a group header is selected.
func (m *Model) groupPreview(g *grid.Grid, x, y, w int) bool {
	gi, _, child := m.cursorGroup()
	if gi == nil || child {
		return false
	}
	g.Put(x, y, gi.label, stb(cFG), w)
	g.Put(x, y+1, i18n.Tf("%d ileti · %d okunmamış", gi.n, gi.unread), st(cDim), w)
	hint := i18n.T("Enter veya →: grubu aç")
	if gi.open {
		hint = "← veya Enter: grubu kapat"
	}
	g.Put(x, y+3, hint, st(cFaint), w)
	g.Put(x, y+4, i18n.T("Shift+G: düz listeye dön"), st(cFaint), w)
	if m.inTrashView() {
		g.Put(x, y+5, i18n.T("m: bu gönderenin tümünü Gelen Kutusu'na geri yükle"), st(cFaint), w)
	}
	return true
}

// onGroupHeader is true when the cursor is on a sender-group row.
func (m *Model) onGroupHeader() bool {
	v := m.visible()
	return m.cursor >= 0 && m.cursor < len(v) && isGroupRow(v[m.cursor])
}

func senderDeleteName(gi *grpInfo, mails []backend.Envelope) string {
	if len(mails) > 0 {
		addr := mails[0].FromAddr
		same := addr != ""
		for _, e := range mails[1:] {
			if !strings.EqualFold(e.FromAddr, addr) {
				same = false
				break
			}
		}
		if same {
			return addr
		}
	}
	if gi != nil && gi.label != "" {
		return gi.label
	}
	if gi != nil && gi.key != "" {
		return gi.key
	}
	return i18n.T("gönderen")
}

// askDeleteSender confirms, then deletes every mail of the sender under
// the cursor in the current folder/view (same move-to-trash or purge
// semantics as d, batched).
func (m *Model) groupMembers(gi *grpInfo) []backend.Envelope {
	if gi == nil {
		return nil
	}
	var out []backend.Envelope
	for _, e := range m.baseList() {
		if e.UID == 0 || isGroupRow(e) {
			continue
		}
		if m.senderKey(e.FromAddr) == gi.key {
			out = append(out, e)
		}
	}
	return out
}

func (m *Model) askDeleteSender() tea.Cmd {
	gi, _, child := m.cursorGroup()
	if child || !m.onGroupHeader() || gi == nil {
		return nil
	}
	mails := m.groupMembers(gi)
	if len(mails) == 0 {
		return nil
	}
	name := senderDeleteName(gi, mails)
	n := len(mails)
	text := fmt.Sprintf("%s göndericisinin %d maili silinsin mi? (e/h)", name, n)
	yes := i18n.T("Evet, sil")
	if m.senderMailsInTrash(mails) {
		text = fmt.Sprintf("%s göndericisinin %d maili kalıcı olarak silinsin mi? (e/h)", name, n)
		yes = i18n.T("Evet, kalıcı sil")
	}
	m.confirm = &confirmBox{strict: true, title: i18n.T("Göndereni sil"), text: text, yes: yes,
		onYes: func() tea.Cmd { return m.deleteSenderMails(mails) }}
	return nil
}

func (m *Model) senderMailsInTrash(mails []backend.Envelope) bool {
	for _, e := range mails {
		ai, fo := m.loc(e)
		if ai >= len(m.accts) {
			continue
		}
		if f := m.accts[ai].folder(fo); f != nil && (f.Role == backend.RoleTrash || backend.RoleByName(f.Name) == backend.RoleTrash) {
			return true
		}
	}
	return false
}

func (m *Model) deleteSenderMails(mails []backend.Envelope) tea.Cmd {
	var groups []bulkGroup
	var purge []backend.Envelope
	byAcct := map[int]int{}
	for _, e := range mails {
		if e.UID == 0 || isPlaceholder(e.UID) {
			continue
		}
		ai, fo := m.loc(e)
		if ai >= len(m.accts) {
			continue
		}
		a := m.accts[ai]
		if f := a.folder(fo); f != nil && (f.Role == backend.RoleTrash || backend.RoleByName(f.Name) == backend.RoleTrash) {
			purge = append(purge, e)
			continue
		}
		trash := a.folderByRole(backend.RoleTrash)
		if trash == "" {
			m.toastInfo(toast.Error, "", a.Cfg.Email+i18n.T(": Çöp Kutusu bulunamadı, hiçbir şey silinmedi."))
			return nil
		}
		gi, ok := byAcct[ai]
		if !ok {
			gi = len(groups)
			byAcct[ai] = gi
			groups = append(groups, bulkGroup{acct: ai, from: fo, trash: trash})
		}
		groups[gi].envs = append(groups[gi].envs, e)
	}
	if len(purge) > 0 && len(groups) == 0 {
		return m.purgeMails(purge)
	}
	if len(groups) == 0 {
		return nil
	}
	return m.startBulkDelete(groups)
}

// restoreSenderMails moves every mail of the selected trash group back to
// that account's inbox. u undoes the whole batch.
func (m *Model) restoreSenderMails() tea.Cmd {
	gi, _, child := m.cursorGroup()
	if child || !m.onGroupHeader() || gi == nil || !m.inTrashView() {
		return nil
	}
	if m.bulk != nil && m.bulk.done+m.bulk.failed < m.bulk.total {
		m.toastInfo(toast.Info, "", i18n.T("İşlem sürüyor…"))
		return nil
	}
	mails := m.groupMembers(gi)
	if len(mails) == 0 {
		return nil
	}
	var groups []bulkGroup
	byAcct := map[int]int{}
	for _, e := range mails {
		if e.UID == 0 || isPlaceholder(e.UID) {
			continue
		}
		ai, fo := m.loc(e)
		if ai >= len(m.accts) {
			continue
		}
		a := m.accts[ai]
		inbox := a.inbox()
		if inbox == "" {
			m.toastInfo(toast.Error, "", a.Cfg.Email+i18n.T(": Gelen Kutusu bulunamadı, hiçbir şey taşınmadı."))
			return nil
		}
		if a.B == nil {
			m.toastInfo(toast.Error, "", a.Cfg.Email+i18n.T(" bağlı değil, hiçbir şey taşınmadı."))
			return nil
		}
		gi, ok := byAcct[ai]
		if !ok {
			gi = len(groups)
			byAcct[ai] = gi
			groups = append(groups, bulkGroup{acct: ai, from: fo, trash: inbox})
		}
		groups[gi].envs = append(groups[gi].envs, e)
	}
	if len(groups) == 0 {
		return nil
	}
	return m.startBulkRestore(groups)
}
