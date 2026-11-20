package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/i18n"
	"termail/internal/localstore"
	"termail/internal/mailmsg"
	"termail/internal/opqueue"
	"termail/internal/toast"
)

// Fast start and fast exit.
//
// Start: nothing slow happens before the first frame. The accounts come
// from config.json; the backend of each (which needs the Windows
// Credential Manager) is built in the background, then it signs in,
// fetches the newest page of the inbox, the folders, and starts push
// (IDLE) — every account in parallel. Meanwhile the inbox list of the last
// run is shown from a small local cache.
//
// Exit: q, Ctrl+C and closing the window never wait for the server.
// Connections are closed directly (no LOGOUT / IDLE DONE / QUIT), changes
// the server has not confirmed are written to a local file and replayed on
// the next start, and a message that was still being written or sent is
// saved locally and reopened next time.

const (
	exitBudget   = 200 * time.Millisecond
	quitSendCap  = 15 * time.Second // q waits at most this long for a send
	closeHandoff = 250 * time.Millisecond
)

type builtMsg struct {
	acct int
	b    backend.Backend
	err  error
	took time.Duration
}

type localMsg struct {
	inbox  map[int]*localstore.Inbox
	ops    []localstore.Op
	drafts []localstore.Draft
}

type quitCapMsg struct{}

// closeMsg: the console window is closing; save what must survive.
type closeMsg struct{ done chan struct{} }

// mark writes a timing line to the log (--zaman / TERMAIL_TIMING=1).
func (m *Model) mark(what string) {
	if m.opt.Timing {
		m.log.Printf("zaman %-40s %8.1f ms", what, float64(time.Since(m.opt.Start).Microseconds())/1000)
	}
}

func (m *Model) markOnce(key, what string) {
	if !m.opt.Timing || m.marked[key] {
		return
	}
	m.marked[key] = true
	m.mark(what)
}

// startAccount brings one account up without blocking the UI.
func (m *Model) startAccount(i int) tea.Cmd {
	a := m.accts[i]
	if a.B != nil {
		return m.connectAccount(i)
	}
	if a.Build == nil {
		a.State = stNeedsLogin
		return nil
	}
	a.State = stConnecting
	build := a.Build
	return func() tea.Msg {
		t := time.Now()
		b, err := build() // reads the Credential Manager: never before the first frame
		return builtMsg{acct: i, b: b, err: err, took: time.Since(t)}
	}
}

func (m *Model) onBuilt(msg builtMsg) tea.Cmd {
	if msg.acct >= len(m.accts) {
		return nil
	}
	a := m.accts[msg.acct]
	m.mark(fmt.Sprintf("hesap %d: giriş bilgisi okundu (%d ms)", msg.acct, msg.took.Milliseconds()))
	if msg.err != nil || msg.b == nil {
		a.State = stNeedsLogin
		m.log.Printf("account %s: needs sign-in", a.Cfg.Email)
		return nil
	}
	a.B = msg.b
	return m.connectAccount(msg.acct)
}

// connectAccount starts push, the first inbox page and the folder list.
func (m *Model) connectAccount(i int) tea.Cmd {
	a := m.accts[i]
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	a.cancel = cancel
	b := a.B
	go b.Watch(ctx, "INBOX", func(e backend.Event) { m.send(watchMsg{acct: i, ev: e}) })
	m.flushReplay(i)
	return tea.Batch(m.loadListPage(i, "INBOX"), m.loadFolders(i), m.scheduleOps())
}

// loadListPage starts a newest-first progressive load of the inbox.
// Cached rows stay on screen; onList merges pages as they arrive.
func (m *Model) loadListPage(i int, folder string) tea.Cmd {
	if i >= len(m.accts) || m.accts[i].B == nil {
		return nil
	}
	m.accts[i].inboxAsked = true
	a := m.accts[i]
	if a.fromCache && a.Loaded[folder] && len(a.Lists[folder]) > 0 && a.hasSyncState(folder) {
		return m.syncOrLoadList(i, folder)
	}
	return m.startListLoad(i, folder, false, 0)
}

// ---- local cache, pending changes, unsent messages ----

func (m *Model) loadLocal() tea.Cmd {
	st := m.opt.Store
	if st == nil {
		return nil
	}
	emails := make([]string, len(m.accts))
	for i, a := range m.accts {
		emails[i] = a.Cfg.Email
	}
	return func() tea.Msg {
		lm := localMsg{inbox: map[int]*localstore.Inbox{}}
		for i, e := range emails {
			if in := st.LoadInbox(e); in != nil {
				lm.inbox[i] = in
			}
		}
		lm.ops = st.TakeOps()
		lm.drafts = st.TakeDrafts()
		return lm
	}
}

func (m *Model) onLocal(lm localMsg) tea.Cmd {
	m.mark("yerel önbellek okundu")
	for i, in := range lm.inbox {
		if i >= len(m.accts) {
			continue
		}
		a := m.accts[i]
		folder := in.Folder
		if folder == "" {
			folder = "INBOX"
		}
		if len(a.Folders) == 0 {
			for _, f := range in.Folders {
				a.Folders = append(a.Folders, backend.Folder{Name: f.Name, Role: backend.Role(f.Role), Delim: f.Delim, Total: f.Total, Unread: f.Unread})
			}
		}
		if a.Loaded[folder] { // the server was faster: its list wins
			continue
		}
		envs := make([]backend.Envelope, 0, len(in.List))
		for _, h := range in.List {
			envs = append(envs, backend.Envelope{UID: h.UID, From: h.From, FromAddr: h.FromAddr, To: h.To, ToAddr: h.ToAddr, ToPeople: h.ToPeople, Subject: h.Subject, Date: h.Date, Seen: h.Seen, Flagged: h.Flagged, Attach: h.Attach, ReadAt: h.ReadAt})
		}
		a.Lists[folder] = m.overlayPending(i, folder, dedupeEnvs(envs))
		a.Loaded[folder] = true
		a.fromCache = true
		if in.UIDValidity != 0 || in.UIDNext != 0 || in.HighestModSeq != 0 {
			if a.folderSync == nil {
				a.folderSync = map[string]backend.FolderSync{}
			}
			a.folderSync[folder] = backend.FolderSync{Folder: folder, UIDValidity: in.UIDValidity, UIDNext: in.UIDNext, HighestModSeq: in.HighestModSeq}
			a.setExpect(folder, "unseen", countUnseen(envs))
			a.setExpect(folder, "seen", len(envs)-countUnseen(envs))
		}
	}
	if len(lm.ops) > 0 {
		m.replay = append(m.replay, lm.ops...)
		for i, a := range m.accts {
			if a.B != nil {
				m.flushReplay(i)
			} else {
				m.applyReplayLocally(i)
			}
		}
	}
	for _, d := range lm.drafts {
		m.reopenDraft(d)
	}
	if n := len(lm.drafts); n > 0 {
		m.active = 0
		m.toasts.Push(toast.Info, "", i18n.Tf("Kapanırken yarım kalan %d ileti sekmede açıldı.", n), m.opt.Now(), 8*time.Second)
	}
	m.clampCursor()
	return tea.Batch(m.previewCmd(), m.scheduleOps())
}

// saveInboxCache writes the inbox rows of an account in the background.
func (m *Model) saveInboxCache(i int) {
	st := m.opt.Store
	if st == nil || i >= len(m.accts) {
		return
	}
	a := m.accts[i]
	folder := a.inbox()
	list := a.Lists[folder]
	in := localstore.Inbox{Email: a.Cfg.Email, Saved: m.opt.Now(), Folder: folder}
	if a.folderSync != nil {
		if st := a.folderSync[folder]; st.UIDValidity != 0 || st.UIDNext != 0 || st.HighestModSeq != 0 {
			in.UIDValidity, in.UIDNext, in.HighestModSeq = st.UIDValidity, st.UIDNext, st.HighestModSeq
		}
	}
	for _, e := range list {
		if len(in.List) >= localstore.MaxCached {
			break
		}
		if e.UID >= 0xFF000000 { // a local placeholder, not a server UID
			continue
		}
		in.List = append(in.List, localstore.Header{UID: e.UID, From: e.From, FromAddr: e.FromAddr, To: e.To, ToAddr: e.ToAddr, ToPeople: e.ToPeople, Subject: e.Subject, Date: e.Date, Seen: e.Seen, Flagged: e.Flagged, Attach: e.Attach, ReadAt: e.ReadAt})
	}
	for _, f := range a.Folders {
		in.Folders = append(in.Folders, localstore.Folder{Name: f.Name, Role: string(f.Role), Delim: f.Delim, Total: f.Total, Unread: f.Unread})
	}
	m.bg.Add(1)
	go func() {
		defer m.bg.Done()
		_ = st.SaveInbox(in)
	}()
}

func (m *Model) acctByEmail(email string) int {
	for i, a := range m.accts {
		if strings.EqualFold(a.Cfg.Email, email) {
			return i
		}
	}
	return -1
}

// holdOp keeps a change made while the account is still connecting; it is
// sent once the account is up (or saved for the next start).
func (m *Model) holdOp(ai int, o opqueue.Op) bool {
	if ai >= len(m.accts) {
		return false
	}
	a := m.accts[ai]
	if a.B != nil || a.Build == nil || a.State == stNeedsLogin {
		return false
	}
	lo := localstore.Op{Email: a.Cfg.Email, Folder: o.Folder, UID: o.UID}
	if o.Kind == opqueue.Move {
		lo.Kind, lo.Dest = "move", o.Dest
	} else {
		lo.Kind, lo.Flag, lo.On = "flag", string(o.Flag), o.On
	}
	m.replay = append(m.replay, lo)
	return true
}

type replayPayload struct{}

// applyReplayLocally shows the stored changes in the (cached) lists.
func (m *Model) applyReplayLocally(ai int) {
	for _, o := range m.replay {
		if m.acctByEmail(o.Email) != ai {
			continue
		}
		switch o.Kind {
		case "move":
			m.removeEnv(ai, o.Folder, o.UID)
		case "flag":
			if backend.Flag(o.Flag) == backend.FlagSeen {
				m.setSeenLocal(ai, o.Folder, o.UID, o.On)
			} else {
				for j, e := range m.accts[ai].Lists[o.Folder] {
					if e.UID == o.UID {
						m.accts[ai].Lists[o.Folder][j].Flagged = o.On
					}
				}
			}
		}
	}
}

// flushReplay queues the stored changes of an account that is now up.
func (m *Model) flushReplay(ai int) {
	if len(m.replay) == 0 || m.accts[ai].B == nil {
		return
	}
	m.applyReplayLocally(ai)
	kept := m.replay[:0]
	now := time.Now()
	for _, o := range m.replay {
		if m.acctByEmail(o.Email) != ai {
			kept = append(kept, o)
			continue
		}
		op := opqueue.Op{Acct: ai, Folder: o.Folder, UID: o.UID}
		if o.Kind == "move" {
			op.Kind, op.Dest = opqueue.Move, o.Dest
		} else {
			op.Kind, op.Flag, op.On = opqueue.Flag, backend.Flag(o.Flag), o.On
		}
		id := m.ops.Add(op, now)
		m.opData[id] = replayPayload{}
	}
	m.replay = kept
}

// pendingOps lists every change the server has not confirmed yet.
func (m *Model) pendingOps() []localstore.Op {
	out := append([]localstore.Op(nil), m.replay...)
	for _, o := range m.ops.All() {
		if o.Acct >= len(m.accts) {
			continue
		}
		lo := localstore.Op{Email: m.accts[o.Acct].Cfg.Email, Folder: o.Folder, UID: o.UID}
		if o.Kind == opqueue.Move {
			lo.Kind, lo.Dest = "move", o.Dest
		} else {
			lo.Kind, lo.Flag, lo.On = "flag", string(o.Flag), o.On
		}
		out = append(out, lo)
	}
	return out
}

func (m *Model) draftOf(c *composer, sending bool) localstore.Draft {
	d := localstore.Draft{To: c.to.Value(), Cc: c.cc.Value(), Subject: c.subject.Value(), Body: c.fullBody(m), Sending: sending}
	if c.from < len(m.accts) {
		d.From = m.accts[c.from].Cfg.Email
	}
	for _, a := range c.atts {
		d.Atts = append(d.Atts, localstore.Attachment{Name: a.Name, ContentType: a.ContentType, Data: a.Data})
	}
	return d
}

// reopenDraft opens a saved unsent message in a compose tab.
func (m *Model) reopenDraft(d localstore.Draft) {
	keep := m.cur
	if i := m.acctByEmail(d.From); i >= 0 {
		m.cur = i
	}
	m.openCompose(composeNew, nil, backend.Envelope{})
	m.cur = keep
	c := m.tabs[len(m.tabs)-1].compose
	if c == nil {
		return
	}
	c.to.SetValue(d.To)
	c.cc.SetValue(d.Cc)
	c.subject.SetValue(d.Subject)
	c.body.SetValue(d.Body)
	for _, a := range d.Atts {
		c.atts = append(c.atts, mailmsg.Attachment{Name: a.Name, ContentType: a.ContentType, Data: a.Data, Size: len(a.Data)})
	}
	if d.Subject != "" {
		c.tab.title = i18n.T("Yarım: ") + trimSubj(d.Subject)
	}
	if d.Sending {
		c.status, c.statusFG = i18n.T("Bu ileti kapanırken gönderiliyordu; gitmiş olabilir. Gönderilenler'e bakıp gerekirse yeniden gönder."), cYellow
	} else {
		c.status, c.statusFG = i18n.T("Kapanırken yarım kalmıştı; kaldığın yerden devam edebilirsin."), cDim
	}
}

// persistForExit saves (locally, quickly) everything a fast exit would
// otherwise lose. It runs once.
func (m *Model) persistForExit() {
	if m.persisted {
		return
	}
	m.persisted = true
	st := m.opt.Store
	if st == nil {
		return
	}
	for _, t := range m.tabs {
		if c := t.compose; c != nil && c.dirty() && !m.inflight[c] {
			_, _ = st.SaveDraft(m.draftOf(c, false))
		}
	}
	for c := range m.inflight {
		_, _ = st.SaveDraft(m.draftOf(c, true))
	}
	_ = st.SaveOps(m.pendingOps())
}

// exitNow leaves at once: save locally, then stop the program without the
// final render or waiting for the keyboard reader (that wait alone is up
// to half a second on Windows).
func (m *Model) exitNow() tea.Cmd {
	m.exitAt = time.Now()
	m.persistForExit()
	m.quitting = true
	if p := m.prog; p != nil {
		return func() tea.Msg { p.Kill(); return nil }
	}
	return tea.Quit
}

// quit is q: leaves at once, unless a message is being sent — then it waits
// for that send (at most quitSendCap, shown in a toast); a second q leaves
// right away and keeps the message locally.
func (m *Model) quit() tea.Cmd {
	if m.sendsPending == 0 {
		return m.exitNow()
	}
	if m.quitWait {
		return m.exitNow()
	}
	m.quitWait = true
	m.toasts.Push(toast.Info, "", i18n.Tf("Gönderiliyor… bitince kapanacak (en çok %d sn). Hemen çıkmak için yine q.", int(quitSendCap.Seconds())), m.opt.Now(), quitSendCap)
	return tea.Tick(quitSendCap, func(time.Time) tea.Msg { return quitCapMsg{} })
}

// closeBackends closes every connection directly and waits at most budget
// (also for cache files still being written); whatever is not done by then
// ends with the process.
func closeBackends(accts []*Acct, bg *sync.WaitGroup, budget time.Duration) time.Duration {
	t := time.Now()
	var wg sync.WaitGroup
	for _, a := range accts {
		if a.B == nil {
			continue
		}
		wg.Add(1)
		go func(b backend.Backend) {
			defer wg.Done()
			b.Close()
		}(a.B)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		if bg != nil {
			bg.Wait()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(budget):
	}
	return time.Since(t)
}

// consoleClosing runs when the window is closed (or the session ends):
// hand over to the UI to save drafts and pending changes, close the
// connections without waiting, and end the process.
func (m *Model) consoleClosing(p *tea.Program, exit func()) {
	t := time.Now()
	done := make(chan struct{})
	go p.Send(closeMsg{done: done})
	select {
	case <-done:
	case <-time.After(closeHandoff):
	}
	m.cancel()
	closeBackends(m.accts, nil, 100*time.Millisecond)
	m.log.Printf("zaman pencere kapandı → çıkış %8.1f ms", float64(time.Since(t).Microseconds())/1000)
	exit()
}

func countUnseen(l []backend.Envelope) int {
	n := 0
	for _, e := range l {
		if !e.Seen {
			n++
		}
	}
	return n
}
