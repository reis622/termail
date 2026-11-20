package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"termail/internal/backend"
	"termail/internal/edit"
	"termail/internal/grid"
	"termail/internal/mailmsg"
	"termail/internal/search"
)

// Search: `/` or Ctrl+F opens the bar above the list. Typing filters the
// mail that is already on this computer (headers and opened mail) at once;
// a moment later the server is asked too, on its own connection, and its
// results are merged in. A newer query cancels the older server search.

const (
	scopeFolder = iota
	scopeAllFolders
	scopeAllAccounts
)

var scopeNames = [...]string{"Bu klasör", "Tüm klasörler", "Tüm hesaplar"}

// serverSearchDelay: the server is asked once typing pauses this long.
const serverSearchDelay = 350 * time.Millisecond

// serverSearchLimit: newest results per folder taken from the server.
const serverSearchLimit = 200

type searchBox struct {
	in      edit.Editor
	focused bool
	busy    bool // server search running
	scope   int
	q       search.Query
	seq     int // bumped on every change of the query or scope
	cancel  context.CancelFunc
	server  []backend.Envelope // server results of srvSeq
	srvSeq  int
	srvErr  string
	rcur    int // highlighted recent search (-1: none)
}

func newSearch() *searchBox {
	in := edit.New(edit.Single)
	in.Focus()
	return &searchBox{in: in, rcur: -1}
}

func (s *searchBox) query() string { return strings.TrimSpace(s.in.Value()) }

type searchKickMsg struct{ seq int }

type searchDoneMsg struct {
	seq  int
	envs []backend.Envelope
	err  error
}

// searching: a search with something to look for is on screen.
func (m *Model) searching() bool { return m.search != nil && !m.search.q.Empty() }

// openSearch shows and focuses the search bar.
func (m *Model) openSearch() tea.Cmd {
	var cmd tea.Cmd
	if m.focus == focusFolders {
		cmd = m.commitPending()
	}
	if m.search == nil {
		m.search = newSearch()
	}
	m.search.focused = true
	m.search.in.Focus()
	m.focus = focusList
	m.clearSel()
	return cmd
}

// closeSearch hides the bar; the highlighted mail stays highlighted when
// it is in the normal list.
func (m *Model) closeSearch() tea.Cmd {
	if m.search == nil {
		return nil
	}
	e, ok := m.selected()
	if m.search.cancel != nil {
		m.search.cancel()
	}
	m.search = nil
	m.cursor, m.offset = 0, 0
	if ok {
		ai, fo := e.Acct, e.Folder
		for i, x := range m.visible() {
			if xa, xf := m.loc(x); x.UID == e.UID && xa == ai && (xf == fo || fo == "") {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
	return m.previewCmd()
}

// dropSearch forgets the search (switching folders / accounts).
func (m *Model) dropSearch() {
	if m.search != nil && m.search.cancel != nil {
		m.search.cancel()
	}
	m.search = nil
}

// searchChanged runs after the query or the scope changed.
func (m *Model) searchChanged() tea.Cmd {
	s := m.search
	s.q = search.Parse(s.in.Value())
	s.seq++
	if s.cancel != nil {
		s.cancel() // the older server search is no longer wanted
		s.cancel = nil
	}
	s.busy, s.server, s.srvErr, s.rcur = false, nil, "", -1
	m.cursor, m.offset = 0, 0
	cmds := []tea.Cmd{m.previewCmd()}
	if !s.q.Empty() && len(m.searchTargets()) > 0 {
		seq := s.seq
		s.busy = true // shown at once; the request itself waits for a pause
		cmds = append(cmds, tea.Tick(serverSearchDelay, func(time.Time) tea.Msg { return searchKickMsg{seq: seq} }))
	}
	return tea.Batch(cmds...)
}

func (s *searchBox) key(m *Model, k tea.KeyMsg) tea.Cmd {
	key := k.String()
	switch key {
	case "esc":
		if s.in.HasSelection() {
			s.in.ClearSelection()
			return nil
		}
		if s.in.Value() != "" {
			s.in.SetValue("")
			return m.searchChanged()
		}
		return m.closeSearch()
	case "enter":
		if s.q.Empty() {
			if s.rcur >= 0 && s.rcur < len(m.prefs.Recent) {
				return m.applySearch(m.prefs.Recent[s.rcur])
			}
			return nil
		}
		if _, ok := m.selected(); !ok {
			return nil
		}
		m.addRecent(s.query())
		s.focused = false
		cmd, _ := m.listKey("enter")
		return cmd
	case "tab", "shift+tab":
		d := 1
		if key == "shift+tab" {
			d = len(scopeNames) - 1
		}
		s.scope = (s.scope + d) % len(scopeNames)
		return m.searchChanged()
	case "up", "down", "pgup", "pgdown":
		if s.q.Empty() {
			n := len(m.prefs.Recent)
			switch key {
			case "down", "pgdown":
				s.rcur = min(n-1, s.rcur+1)
			default:
				s.rcur = max(-1, s.rcur-1)
			}
			return nil
		}
		cmd, _ := m.listKey(key)
		return cmd
	case "ctrl+f":
		return nil
	}
	before := s.in.Value()
	_, cmd := m.editKey(&s.in, k)
	if s.in.Value() == before {
		return cmd // moving the cursor / selecting does not restart the search
	}
	return tea.Batch(cmd, m.searchChanged())
}

// applySearch puts text into the bar (a recent search, a filter chip).
func (m *Model) applySearch(q string) tea.Cmd {
	s := m.search
	s.in.SetValue(q)
	s.in.CursorEnd()
	s.focused = true
	s.in.Focus()
	return m.searchChanged()
}

// insertFilter adds a filter word from the hint chips to the query.
func (m *Model) insertFilter(word string) tea.Cmd {
	v := strings.TrimRight(m.search.in.Value(), " ")
	if v != "" {
		v += " "
	}
	v += word
	if !strings.HasSuffix(word, ":") {
		v += " "
	}
	return m.applySearch(v)
}

// ---- results ----

type foldKey struct {
	acct   int
	folder string
	uid    uint32
}

type foldVal struct{ src, hay, from string }

// searchItem is a mail in the form the matcher wants (cached).
func (m *Model) searchItem(e backend.Envelope) search.Item {
	k := foldKey{e.Acct, e.Folder, e.UID}
	src := e.From + "\x00" + e.FromAddr + "\x00" + e.Subject
	v, ok := m.foldCache[k]
	if !ok || v.src != src {
		if m.foldCache == nil || len(m.foldCache) > 20000 {
			m.foldCache = make(map[foldKey]foldVal, 512)
		}
		from := search.Fold(e.From + " " + e.FromAddr)
		v = foldVal{src: src, from: from, hay: from + " " + search.Fold(e.Subject)}
		m.foldCache[k] = v
	}
	it := search.Item{FromHay: v.from, Hay: v.hay, Date: e.Date, Seen: e.Seen, Flagged: e.Flagged, Attach: e.Attach}
	if p, ok := m.bodies.Peek(bodyKey{e.Acct, e.Folder, e.UID}); ok && p != nil {
		b, ok := m.bodyFold[p]
		if !ok {
			if m.bodyFold == nil || len(m.bodyFold) > 300 {
				m.bodyFold = map[*mailmsg.Parsed]string{}
			}
			b = search.Fold(p.Text)
			m.bodyFold[p] = b
		}
		it.Body = b
		if len(p.Attachments) > 0 {
			it.Attach = true
		}
	}
	return it
}

// baseList is the list on screen without search or grouping, with every
// row's account and folder filled in.
func (m *Model) baseList() []backend.Envelope {
	a := m.acct()
	if a == nil {
		return nil
	}
	switch m.virt {
	case viewUnified:
		return m.splitRead(mergeInboxes(m.accts), 0, "")
	case viewSaved:
		return mergeSaved(m.accts)
	}
	l := a.Lists[m.folder]
	if m.folder == a.inbox() {
		return m.splitRead(l, m.cur, m.folder)
	}
	if f := a.folder(m.folder); f != nil && f.Role == backend.RoleFlagged && len(l) == 0 {
		return nil
	}
	return l
}

// folderOrder lists an account's folders for "all folders": inbox first,
// the usual folders, own folders, and "All Mail" last (so a mail found in
// the inbox and in All Mail shows as the inbox copy).
func folderOrder(a *Acct) []string {
	var out []string
	for _, r := range backend.RoleOrder {
		for _, f := range a.Folders {
			if f.Role == r && r != backend.RoleAll && r != backend.RoleFlagged {
				out = append(out, f.Name)
			}
		}
	}
	for _, f := range a.Folders {
		if f.Role == backend.RoleNone {
			out = append(out, f.Name)
		}
	}
	if n := a.folderByRole(backend.RoleAll); n != "" {
		out = append(out, n)
	}
	seen := map[string]bool{}
	for _, f := range out {
		seen[f] = true
	}
	for name := range a.Lists { // lists we have whose folder is not known (yet)
		if !seen[name] {
			out = append(out, name)
		}
	}
	return out
}

// searchPool is every mail on this computer the search scope covers.
func (m *Model) searchPool() []backend.Envelope {
	s := m.search
	scope := s.scope
	if scope == scopeFolder {
		l := m.baseList()
		if m.virt == viewFolder {
			out := make([]backend.Envelope, len(l))
			for i, e := range l {
				e.Acct, e.Folder = m.cur, m.folder
				out[i] = e
			}
			return out
		}
		return l
	}
	accts := []int{m.cur}
	if scope == scopeAllAccounts || m.virt != viewFolder {
		accts = accts[:0]
		for i := range m.accts {
			accts = append(accts, i)
		}
	}
	var out []backend.Envelope
	for _, ai := range accts {
		a := m.accts[ai]
		for _, fo := range folderOrder(a) {
			for _, e := range a.Lists[fo] {
				e.Acct, e.Folder = ai, fo
				out = append(out, e)
			}
		}
	}
	return out
}

type idKey struct {
	acct int
	id   string
}

// searchResults: the local matches plus the server's, without duplicates
// (same mail in two folders, e.g. Gmail's inbox and All Mail), newest first.
func (m *Model) searchResults() []backend.Envelope {
	s := m.search
	now := m.opt.Now()
	var out []backend.Envelope
	seen := map[foldKey]bool{}
	seenID := map[idKey]bool{}
	add := func(e backend.Envelope) {
		k := foldKey{e.Acct, e.Folder, e.UID}
		if seen[k] {
			return
		}
		if e.MessageID != "" {
			ik := idKey{e.Acct, e.MessageID}
			if seenID[ik] {
				return
			}
			seenID[ik] = true
		}
		seen[k] = true
		out = append(out, e)
	}
	for _, e := range m.searchPool() {
		if s.q.Match(m.searchItem(e), now) {
			add(e)
		}
	}
	if s.srvSeq == s.seq {
		for _, e := range s.server {
			if m.searchVerified(e, s.q, now) {
				add(e)
			}
		}
	}
	// sender matches first, then subject, then text only; newest first in each
	tier := make(map[foldKey]int, len(out))
	for _, e := range out {
		tier[foldKey{e.Acct, e.Folder, e.UID}] = s.q.Tier(m.searchItem(e))
	}
	sortMerged(out)
	sort.SliceStable(out, func(i, j int) bool {
		return tier[foldKey{out[i].Acct, out[i].Folder, out[i].UID}] < tier[foldKey{out[j].Acct, out[j].Folder, out[j].UID}]
	})
	return out
}

// ---- server search ----

type searchTarget struct {
	acct    int
	b       backend.Backend
	folders []string
}

// searchTargets: which folders of which accounts the server searches.
func (m *Model) searchTargets() []searchTarget {
	s := m.search
	var out []searchTarget
	addAcct := func(ai int, folders []string) {
		a := m.accts[ai]
		if a.B == nil || len(folders) == 0 {
			return
		}
		out = append(out, searchTarget{acct: ai, b: a.B, folders: folders})
	}
	all := func(a *Acct) []string {
		if n := a.folderByRole(backend.RoleAll); n != "" {
			return []string{n} // Gmail: every label is in All Mail; one search, no duplicates
		}
		var fs []string
		for _, f := range folderOrder(a) {
			if a.folder(f) != nil {
				fs = append(fs, f)
			}
		}
		return fs
	}
	switch {
	case s.scope == scopeFolder && m.virt == viewFolder:
		addAcct(m.cur, []string{m.folder})
	case s.scope == scopeFolder && m.virt == viewUnified:
		for i, a := range m.accts {
			addAcct(i, []string{a.inbox()})
		}
	case s.scope == scopeFolder: // Kaydedilenler: what is on this computer
	case s.scope == scopeAllFolders && m.virt == viewFolder:
		addAcct(m.cur, all(m.accts[m.cur]))
	default:
		for i, a := range m.accts {
			addAcct(i, all(a))
		}
	}
	return out
}

func criteriaOf(q search.Query, now time.Time) backend.Criteria {
	since, before := q.Range(now)
	return backend.Criteria{Text: q.Words, From: q.From, Since: since, Before: before,
		Unseen: q.Unread, Flagged: q.Saved, Attach: q.Attach}
}

func (m *Model) startServerSearch(seq int) tea.Cmd {
	s := m.search
	if s == nil || seq != s.seq || s.q.Empty() {
		return nil
	}
	targets := m.searchTargets()
	if len(targets) == 0 {
		s.busy = false
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
	s.cancel, s.busy = cancel, true
	crit := criteriaOf(s.q, m.opt.Now())
	return func() tea.Msg {
		defer cancel()
		var mu sync.Mutex
		var all []backend.Envelope
		var firstErr error
		var wg sync.WaitGroup
		for _, t := range targets { // accounts in parallel, folders one after another
			wg.Add(1)
			go func(t searchTarget) {
				defer wg.Done()
				for _, fo := range t.folders {
					envs, err := backend.SearchBy(ctx, t.b, fo, crit, serverSearchLimit)
					mu.Lock()
					if err != nil && firstErr == nil {
						firstErr = err
					}
					for _, e := range envs {
						e.Acct, e.Folder = t.acct, fo
						all = append(all, e)
					}
					mu.Unlock()
					if ctx.Err() != nil {
						return
					}
				}
			}(t)
		}
		wg.Wait()
		return searchDoneMsg{seq: seq, envs: all, err: firstErr}
	}
}

func (m *Model) onSearchDone(msg searchDoneMsg) tea.Cmd {
	s := m.search
	if s == nil || msg.seq != s.seq {
		return nil // an older query
	}
	s.busy, s.cancel = false, nil
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		m.logErr("search", msg.err)
		s.srvErr = "sunucuda aranamadı"
	}
	// keep the highlighted mail highlighted while results arrive
	cur, ok := m.selected()
	for i := range msg.envs { // local saved / read changes not yet settled win
		m.applyMarks(msg.envs[i].Acct, msg.envs[i].Folder, msg.envs[i:i+1], time.Time{})
	}
	s.server, s.srvSeq = msg.envs, msg.seq
	if ok {
		for i, e := range m.visible() {
			if e.UID == cur.UID && e.Acct == cur.Acct && e.Folder == cur.Folder {
				m.cursor = i
				break
			}
		}
	}
	m.clampCursor()
	return m.previewCmd()
}

// patchSearch keeps server results in step with changes made elsewhere.
func (m *Model) patchSearch(ai int, folder string, uid uint32, fn func(e *backend.Envelope) bool) {
	if m.search == nil {
		return
	}
	l := m.search.server
	for i := 0; i < len(l); i++ {
		if l[i].Acct == ai && l[i].Folder == folder && l[i].UID == uid {
			if !fn(&l[i]) { // false: remove
				l = append(l[:i:i], l[i+1:]...)
				i--
			}
		}
	}
	m.search.server = l
}

// ---- drawing ----

func searchHL(g *grid.Grid, x, y int, text string, needles []string) {
	hlCells(g, x, y, text, needles, runewidth.RuneWidth)
}

// hlCells paints the cells where needles match in text, which was drawn
// at x, y (cellW: the width each rune took when drawn).
func hlCells(g *grid.Grid, x, y int, text string, needles []string, cellW func(rune) int) {
	sp := search.Spans(text, needles)
	if sp == nil {
		return
	}
	ri, cx, si := 0, x, 0
	for _, r := range text {
		w := cellW(r)
		for si < len(sp) && ri >= sp[si][1] {
			si++
		}
		if si >= len(sp) {
			return
		}
		if ri >= sp[si][0] {
			for k := 0; k < w; k++ {
				if c := g.At(cx+k, y); c != nil {
					c.FG, c.BG, c.Bold = cBG, cYellow, true
				}
			}
		}
		cx += w
		ri++
	}
}

// chip draws a filter label; on = in use.
func chip(g *grid.Grid, x, y int, text string, on bool) int {
	s := stbg(cFG, cRaised)
	if on {
		s = stbgb(cBG, cAccent, true)
	}
	return g.Put(x, y, " "+text+" ", s, 0)
}

func (m *Model) searchTitle() string {
	switch m.search.scope {
	case scopeAllAccounts:
		return "Arama · tüm hesaplar"
	case scopeAllFolders:
		if m.virt == viewFolder {
			if a := m.acct(); a != nil {
				return "Arama · " + a.Cfg.Name + " · tüm klasörler"
			}
		}
		return "Arama · tüm klasörler"
	}
	return "Arama · " + m.listTitle()
}

// drawSearchList draws the list panel while the search bar is open.
func (m *Model) drawSearchList(g *grid.Grid, x0, top, w, h int, focused bool) {
	s := m.search
	mx, mw := x0+2, w-4
	bg := cSel
	if s.focused {
		bg = cSelFocus
	}
	g.Fill(x0+1, top+1, w-2, 1, bg)
	g.Put(mx, top+1, "Ara", stbgb(cAccent, bg, true), 0)
	lab := " " + scopeNames[s.scope] + " "
	lw := grid.Width(lab)
	g.RPut(mx+mw, top+1, lab, stbgb(cBG, cAccent, true))
	m.hits.add(hit{x: mx + mw - lw, y: top + 1, w: lw, h: 1, kind: hitScope})
	fw := mw - 4 - lw - 1
	if s.focused {
		es := editStyle(bg)
		es.FG = cFG
		s.in.Draw(g, mx+4, top+1, fw, 1, es)
	} else {
		g.Put(mx+4, top+1, s.in.Value(), stbgb(cFG, bg, true), fw)
	}
	m.hits.add(hit{x: mx, y: top + 1, w: mw - lw, h: 1, kind: hitSearch, ox: mx + 4, oy: top + 1})
	y := top + 2
	if s.q.Empty() {
		m.drawSearchHints(g, mx, top+3, mw, top+h-1)
		return
	}
	if s.q.HasFilters() {
		x := mx
		for _, c := range s.q.Chips() {
			if x+grid.Width(c)+2 > mx+mw {
				break
			}
			x = chip(g, x, y, c, true) + 1
		}
		y++
	}
	v := m.visible()
	cx := g.Put(mx, y, fmt.Sprintf("%d sonuç", len(v)), st(cDim), 0)
	switch {
	case s.busy:
		g.Put(cx+2, y, "·  sunucuda aranıyor…", st(cFaint), mx+mw-cx-2)
	case s.srvErr != "":
		g.Put(cx+2, y, "·  "+s.srvErr, st(cYellow), mx+mw-cx-2)
	}
	y++
	g.HLine(x0+1, y, w-2, cBorder, "", "")
	y0 := y + 1
	rowsH := top + h - 1 - y0
	if len(v) == 0 {
		m.cursor, m.offset = 0, 0
		msg := "Sonuç yok."
		if s.busy {
			msg = "Bu bilgisayarda yok; sunucuda aranıyor…"
		}
		g.Put(mx, y0+1, msg, st(cFaint), mw)
		return
	}
	m.clampCursorTo(len(v))
	const rh = 3 // row, snippet, gap
	per := max(1, (rowsH+1)/rh)
	if !m.freeScroll {
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+per {
			m.offset = m.cursor - per + 1
		}
	}
	m.offset = max(0, min(m.offset, max(0, len(v)-per)))
	needles := s.q.Needles()
	multi := s.scope == scopeAllAccounts || m.virt != viewFolder
	for i := m.offset; i < len(v) && i-m.offset < per; i++ {
		e := v[i]
		y := y0 + (i-m.offset)*rh
		m.hits.add(hit{x: x0 + 1, y: y, w: w - 2, h: 2, kind: hitListRow, idx: i})
		var rbg string
		if i == m.cursor {
			rbg = cSelFocus
			g.Fill(x0+1, y, w-2, 2, rbg)
		}
		fx, sx := m.drawMailRow(g, mx, mw, y, i, e, rbg, 0, multi, needles)
		_ = sx
		snip := m.snippet(e, s.q.Words, mx+mw-fx)
		g.Put(fx, y+1, snip, stbg(cFaint, rbg), mx+mw-fx)
		searchHL(g, fx, y+1, grid.Truncate(snip, mx+mw-fx), needles)
	}
}

// snippet is a line of the mail text around the first match (the sender's
// address when the text is not on this computer).
func (m *Model) snippet(e backend.Envelope, needles []string, width int) string {
	if p, ok := m.bodies.Peek(bodyKey{e.Acct, e.Folder, e.UID}); ok && p != nil {
		if len(needles) == 0 { // only filters: the start of the text
			if t := strings.Join(strings.Fields(p.Text), " "); t != "" {
				return t
			}
		}
		if sn := search.Snippet(p.Text, needles, width); sn != "" {
			return sn
		}
	}
	if loc := m.searchMatchLoc(e, needles); loc != "" {
		return loc
	}
	return ""
}

// searchVerified reports whether a (usually server) hit is a real match
// against the subject, sender, or body when the body is on this computer.
// Hits that only matched hidden server-side text, with nothing visible to
// show, are dropped.
func (m *Model) searchVerified(e backend.Envelope, q search.Query, now time.Time) bool {
	it := m.searchItem(e)
	if q.Match(it, now) && (it.Body != "" || searchHeadersMatch(it, q)) {
		return true
	}
	// no body, and headers don't match: don't list an unverified IMAP hit
	return false
}

func searchHeadersMatch(it search.Item, q search.Query) bool {
	if len(q.From) == 0 && len(q.Words) == 0 {
		return true // filter-only query; headers/flags already passed Match
	}
	for _, f := range q.From {
		if !strings.Contains(it.FromHay, search.Fold(f)) {
			return false
		}
	}
	if len(q.Words) == 0 {
		return len(q.From) > 0 || q.HasFilters()
	}
	for _, w := range q.Words {
		if !strings.Contains(it.Hay, search.Fold(w)) {
			return false
		}
	}
	return true
}

// searchMatchLoc is a short line saying where the query matched when the
// body snippet is not available (subject or sender).
func (m *Model) searchMatchLoc(e backend.Envelope, needles []string) string {
	if len(needles) == 0 {
		return ""
	}
	for _, n := range needles {
		if n != "" && containsFold(e.Subject, n) {
			return "Konu: " + e.Subject
		}
	}
	for _, n := range needles {
		if n != "" && (containsFold(e.From, n) || containsFold(e.FromAddr, n)) {
			return "Gönderen: " + e.From + " <" + e.FromAddr + ">"
		}
	}
	return ""
}

func containsFold(s, needle string) bool {
	return strings.Contains(search.Fold(s), search.Fold(needle))
}

func (m *Model) drawSearchHints(g *grid.Grid, mx, y, mw, bottom int) {
	s := m.search
	if len(m.prefs.Recent) > 0 {
		g.Put(mx, y, "Son aramalar", st(cFaint), 0)
		y++
		for i, q := range m.prefs.Recent {
			if y >= bottom-6 {
				break
			}
			var bg string
			if i == s.rcur {
				bg = cSelFocus
				g.Fill(mx-1, y, mw+2, 1, bg)
			}
			g.Put(mx, y, "·", stbg(cFaint, bg), 0)
			g.Put(mx+2, y, q, stbg(cFG, bg), mw-2)
			m.hits.add(hit{x: mx - 1, y: y, w: mw + 2, h: 1, kind: hitRecent, idx: i})
			y++
		}
		y++
	}
	g.Put(mx, y, "Filtreler", st(cFaint), 0)
	y++
	x := mx
	for i, f := range search.FilterHints {
		cw := grid.Width(f) + 2
		if x+cw > mx+mw || i == 4 { // two rows like the preview
			x, y = mx, y+1
		}
		m.hits.add(hit{x: x, y: y, w: cw, h: 1, kind: hitChip, idx: i})
		x = chip(g, x, y, f, false) + 1
	}
	y += 2
	g.Put(mx, y, "Yazmaya başlayın; sonuçlar anında görünür.", st(cFaint), mw)
	y++
	g.Put(mx, y+1, "Tab: bu klasör · tüm klasörler · tüm hesaplar", st(cFaint), mw)
}
