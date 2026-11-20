package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"termail/internal/auth"
	"termail/internal/edit"
	"termail/internal/grid"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/blocklist"
	"termail/internal/toast"
	"termail/internal/undo"
)

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	m.noRender = false // only an idle mouse event (handled below) skips the frame
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.Type == tea.KeyRunes && !k.Paste && !anyPrintable(k.Runes) {
			return m, nil // Ctrl / AltGr alone on some consoles: NUL, never text
		}
		msg = textKey(k)
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w, h := msg.Width, msg.Height
		if tw, th, ok := termSizeFn(); ok { // Windows reports the buffer size; trust the window
			w, h = tw, th
		}
		return m, m.applySize(w, h)
	case tickMsg:
		m.toasts.Tick(m.opt.Now())
		if tw, th, ok := termSizeFn(); ok && (tw != m.realW || th != m.realH) {
			cmds = append(cmds, m.applySize(tw, th)) // resize events are unreliable in some Windows hosts
		}
		if m.login != nil {
			cmds = append(cmds, m.login.tick())
		}
		return m, tea.Batch(append(cmds, tick())...)
	case builtMsg:
		return m, m.onBuilt(msg)
	case localMsg:
		return m, m.onLocal(msg)
	case quitCapMsg:
		if m.quitWait && !m.quitting {
			return m, m.exitNow()
		}
		return m, nil
	case closeMsg:
		m.persistForExit()
		m.quitting = true
		close(msg.done)
		return m, nil
	case InjectMsg:
		return m, tea.Batch(msg.Fn(m), m.waitEvent())
	case watchMsg:
		return m, tea.Batch(m.onWatch(msg), m.waitEvent())
	case foldersMsg:
		return m, m.onFolders(msg)
	case listMsg:
		return m, m.onList(msg)
	case syncDeltaMsg:
		return m, m.onSyncDelta(msg)
	case bodyMsg:
		delete(m.loading, msg.key)
		if msg.err != nil {
			m.bodyErr[msg.key] = friendly(msg.err)
			m.logErr("body", msg.err)
			return m, m.flushWantBrowser(msg.key, nil, msg.err)
		}
		m.bodies.Put(msg.key, msg.p)
		delete(m.bodyErr, msg.key)
		return m, tea.Batch(m.flushWantBrowser(msg.key, msg.p, nil), m.prefetchVisible())
	case openBrowserDone:
		return m, m.onOpenBrowserDone(msg)
	case seenTimerMsg:
		return m, nil // highlighting no longer marks read (kept for old timers)
	case bodyLoadMsg:
		if !m.selectionCurrent(msg.key, msg.seq) {
			return m, nil // the cursor moved on; its own timer will load it
		}
		return m, tea.Batch(m.loadBody(msg.key), m.prefetchVisible())
	case moveDoneMsg:
		return m, m.onMoveDone(msg)
	case folderPreviewMsg:
		return m, m.onFolderPreview(msg)
	case indexReadyMsg:
		if t := m.tabs[m.active]; t != nil && t.compose != nil && t.compose.picker != nil {
			t.compose.picker.refresh()
		}
		return m, nil
	case emptyCountMsg:
		return m, m.onEmptyCount(msg)
	case emptyProgressMsg:
		m.onEmptyProgress(msg)
		return m, m.waitEvent()
	case emptyDoneMsg:
		return m, m.onEmptyDone(msg)
	case opBatchMsg:
		return m, m.onOpBatch(msg)
	case folderStatusMsg:
		return m, m.onFolderStatus(msg)
	case opsTickMsg:
		m.opsTimer = false
		return m, m.scheduleOps()
	case attOpenMsg:
		m.onAttOpen(msg)
		return m, nil
	case bulkUndoDoneMsg:
		return m, m.onBulkUndoDone(msg)
	case undoDoneMsg:
		if msg.err != nil {
			m.toastErr("undo", msg.err)
			return m, nil
		}
		m.toastInfo(toast.Success, "", "Geri alındı")
		uid := msg.e.OrigUID
		if msg.newUID != 0 {
			m.retargetUID(msg.e.Account, msg.e.From, msg.e.OrigUID, msg.newUID)
			uid = msg.newUID
		}
		if env, ok := msg.e.Data.(backend.Envelope); ok {
			env.UID = uid
			m.revealUndone(msg.e.Account, msg.e.From, env)
		}
		return m, tea.Batch(m.loadList(msg.e.Account, msg.e.From, true), m.loadFolders(msg.e.Account))
	case flagDoneMsg:
		return m, m.onFlagDone(msg)
	case savedListMsg:
		return m, m.onSavedList(msg)
	case savedMsg:
		if msg.err != nil {
			m.toastErr("save", msg.err)
		} else {
			m.toasts.Push(toast.Success, "Kaydedildi:", msg.path, m.opt.Now(), 5*time.Second)
		}
		return m, nil
	case sendDoneMsg:
		cmd := m.onSendDone(msg)
		if m.quitWait && m.sendsPending == 0 && !m.quitting {
			return m, tea.Batch(cmd, m.exitNow()) // q was pressed during the send
		}
		return m, cmd
	case searchKickMsg:
		return m, m.startServerSearch(msg.seq)
	case searchDoneMsg:
		return m, m.onSearchDone(msg)
	case folderCreatedMsg:
		if msg.err != nil {
			m.removeOptimisticFolder(msg.acct, msg.name)
			m.toastErr("create folder", msg.err)
			return m, nil
		}
		var then tea.Cmd
		if msg.then != nil {
			then = msg.then()
		}
		return m, tea.Batch(m.loadFolders(msg.acct), then)
	case folderGoneMsg:
		if msg.err != nil {
			m.toastErr("delete folder", msg.err)
			return m, m.loadFolders(msg.acct)
		}
		m.toastInfo(toast.Success, "", "Klasör silindi.")
		return m, m.loadFolders(msg.acct)
	case folderRenamedMsg:
		if msg.err != nil {
			m.toastErr("rename folder", msg.err)
			return m, m.loadFolders(msg.acct)
		}
		m.toastInfo(toast.Success, "", "Klasör yeniden adlandırıldı.")
		return m, m.loadFolders(msg.acct)
	case copyDoneMsg:
		return m, m.onCopyDone(msg)
	case loginDoneMsg:
		return m, m.onLoginDone(msg)
	case tea.KeyMsg:
		return m, m.onKey(msg)
	case dragTickMsg:
		return m, m.onDragTick()
	case tea.MouseMsg: // (before fmt.Stringer: mouse events are Stringers too)
		return m, m.onMouse(msg)
	case fmt.Stringer:
		// Ctrl+digit arrives as an escape sequence Bubble Tea does not know
		// (kitty keyboard protocol / xterm modifyOtherKeys).
		if k, ok := ctrlDigitFromCSI(msg.String()); ok {
			return m, m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
	if m.login != nil {
		return m, m.login.update(msg)
	}
	return m, nil
}

func (m *Model) onWatch(w watchMsg) tea.Cmd {
	if w.acct >= len(m.accts) {
		return nil
	}
	a := m.accts[w.acct]
	switch w.ev.Kind {
	case backend.EvOnline:
		a.State, a.Err = stOnline, ""
		a.fullAsked = true
		m.markOnce(fmt.Sprint("idle", w.acct), fmt.Sprintf("hesap %d: anlık bildirim (IDLE) açık", w.acct))
		// The initial inbox load is already running (connectAccount). A
		// check=true reload here used to bump syncGen and then skip later
		// pages, leaving the list on the first 250 for minutes.
		if a.paging || (a.inboxAsked && !a.Loaded[a.inbox()]) {
			return nil
		}
		return m.syncOrLoadList(w.acct, a.inbox())
	case backend.EvOffline:
		a.State = stOffline
		m.logErr("watch", w.ev.Err)
		return nil
	case backend.EvNewMail, backend.EvChanged:
		if a.paging {
			a.needRefresh = true
			return m.loadFolders(w.acct)
		}
		return tea.Batch(m.syncOrLoadList(w.acct, a.inbox()), m.loadFolders(w.acct))
	}
	return nil
}

func (m *Model) onFolders(msg foldersMsg) tea.Cmd {
	if msg.acct >= len(m.accts) {
		return nil
	}
	a := m.accts[msg.acct]
	if msg.err != nil {
		if a.State != stOnline {
			a.State = stOffline
		}
		if auth.SessionExpired(msg.err) {
			a.State = stNeedsLogin // Microsoft sign-in ran out: ask again
		}
		a.Err = friendly(msg.err)
		if errors.Is(msg.err, backend.ErrAuth) && !auth.SessionExpired(msg.err) {
			// e.g. a changed password, or one saved with junk by an older
			// version: signing in again with the same address replaces it
			a.State = stNeedsLogin
			a.Err += " Şifreyi yeniden girmek için + tuşuna bas ve aynı adresle giriş yap."
		}
		m.logErr("folders", msg.err)
		return nil
	}
	first := !a.connected
	a.connected = true
	a.Folders = dedupeFolders(msg.folders) // replace, never append
	m.reconcileFolderCounts(msg.acct)      // keep optimistic counts while moves are in flight
	if a.State == stConnecting {
		a.State = stOnline
	}
	if first {
		m.markOnce(fmt.Sprint("folders", msg.acct), fmt.Sprintf("hesap %d: klasörler geldi", msg.acct))
		var cmds []tea.Cmd
		if msg.acct == m.cur && m.virt == viewFolder && (m.folder == "" || strings.EqualFold(m.folder, "INBOX")) {
			m.folder = a.inbox()
		}
		if !a.inboxAsked || a.inbox() != "INBOX" {
			cmds = append(cmds, m.loadList(msg.acct, a.inbox(), false))
		}
		cmds = append(cmds, m.loadSaved(msg.acct))
		return tea.Batch(cmds...)
	}
	return nil
}

func (m *Model) onList(msg listMsg) tea.Cmd {
	if msg.acct >= len(m.accts) {
		return nil
	}
	a := m.accts[msg.acct]
	if msg.gen != 0 && msg.gen != a.syncGen {
		return nil // a newer load of this account superseded this page
	}
	if msg.err != nil {
		m.logErr("list", msg.err)
		if !msg.check {
			m.toastErr("list", msg.err)
			a.paging = false
		}
		return nil
	}
	// keep the cursor on the same message after a refresh
	var keepUID uint32
	keepAcct := -1
	keepGroup := ""
	if m.showing(msg.acct, msg.folder) {
		if e, ok := m.selected(); ok {
			keepUID = e.UID
			keepAcct, _ = m.loc(e)
		} else if gi, _, child := m.cursorGroup(); gi != nil && !child {
			keepGroup = gi.key
		}
	}
	if p := m.pendingF; p != nil && p.acct == msg.acct && p.folder == msg.folder && msg.req != 0 && msg.req == p.req {
		m.pendingF = nil // the previewed folder arrived: show it now
		m.switchFolderView(msg.acct, msg.folder, p.read)
	}
	msg.envs = m.overlayPending(msg.acct, msg.folder, dedupeEnvs(msg.envs))
	msg.envs = m.applyMarks(msg.acct, msg.folder, msg.envs, msg.asked) // a stale snapshot never undoes a local change
	if msg.page {
		// keep the cached / already shown rows; a small first page must not
		// wipe a longer list (Gelen Kutusu would otherwise shrink to the
		// unread among only the newest few).
		a.Lists[msg.folder] = mergeEnvs(a.Lists[msg.folder], msg.envs)
	} else {
		a.Lists[msg.folder] = msg.envs // replace, never append
	}
	a.Loaded[msg.folder] = true
	if msg.total > 0 {
		a.setExpect(msg.folder, msg.kind, msg.total)
	}
	var cmds []tea.Cmd
	if msg.folder == a.inbox() {
		if !msg.page || len(msg.envs) > 0 {
			a.fromCache = false
		}
		m.markOnce(fmt.Sprint("inbox", msg.acct), fmt.Sprintf("hesap %d: gelen kutusu sayfa (%d ileti)", msg.acct, len(a.Lists[msg.folder])))
	}
	if msg.page {
		next := msg.offset + len(msg.envs)
		a.setPageOff(msg.folder, msg.kind, next)
	}
	if msg.page && !msg.check {
		next := msg.offset + len(msg.envs)
		if msg.total > 0 && next < msg.total {
			a.paging = true
			a.pageFolder, a.pageKind = msg.folder, msg.kind
			cmds = append(cmds, m.pipelinePages(msg.acct, msg.folder, msg.kind, next, msg.total, msg.gen)...)
		} else if msg.kind == "unseen" && msg.folder == a.inbox() && msg.gen == a.syncGen {
			// after all unread pages, fill Okunanlar in the background
			a.paging = true
			a.pageFolder, a.pageKind = msg.folder, "seen"
			if a.pageQueued != nil {
				a.pageQueued[pageKey(msg.folder, "seen")] = 0
			}
			cmds = append(cmds, m.pipelinePages(msg.acct, msg.folder, "seen", 0, a.expectOf(msg.folder, "seen"), msg.gen)...)
		} else if msg.kind == "seen" && msg.folder == a.inbox() && msg.gen == a.syncGen && a.expectOf(msg.folder, "unseen") == 0 && !(m.showing(msg.acct, msg.folder) && m.readView) {
			a.paging = true
			a.pageFolder, a.pageKind = msg.folder, "unseen"
			if a.pageQueued != nil {
				a.pageQueued[pageKey(msg.folder, "unseen")] = 0
			}
			cmds = append(cmds, m.pipelinePages(msg.acct, msg.folder, "unseen", 0, a.expectOf(msg.folder, "unseen"), msg.gen)...)
		} else {
			a.paging = false
			m.captureFolderSync(msg.acct, msg.folder)
			if a.needRefresh && msg.folder == a.inbox() {
				a.needRefresh = false
				cmds = append(cmds, m.syncOrLoadList(msg.acct, msg.folder))
			}
		}
	}
	if msg.folder == a.inbox() {
		var maxUID uint32
		for _, e := range msg.envs {
			maxUID = max(maxUID, e.UID)
		}
		blockedUIDs := map[uint32]bool{}
		if a.inboxPrimed {
			for i := len(msg.envs) - 1; i >= 0; i-- {
				e := msg.envs[i]
				if e.UID <= a.inboxMax || e.Seen {
					continue
				}
				if _, blocked := m.opt.Blocks.Match(a.Cfg.Email, e.FromAddr); blocked {
					cmds = append(cmds, m.autoBlock(msg.acct, msg.folder, e))
					blockedUIDs[e.UID] = true
					continue
				}
				m.toasts.AddMail(toast.Mail{Account: msg.acct, AccountName: a.Cfg.Name + " · " + a.Kind(),
					Folder: msg.folder, UID: e.UID, From: e.From, Subject: e.Subject, At: m.opt.Now()})
			}
		}
		if len(blockedUIDs) > 0 { // hide them right away; the move runs in the background
			var kept []backend.Envelope
			f := a.folder(msg.folder)
			for _, e := range msg.envs {
				if !blockedUIDs[e.UID] {
					kept = append(kept, e)
				} else if f != nil && f.Total > 0 {
					f.Total--
					if !e.Seen && f.Unread > 0 {
						f.Unread--
					}
				}
			}
			a.Lists[msg.folder] = kept
		}
		if maxUID > a.inboxMax {
			a.inboxMax = maxUID
		}
		a.inboxPrimed = true
	}
	if keepUID != 0 {
		for i, e := range m.visible() {
			if ai, _ := m.loc(e); !isGroupRow(e) && e.UID == keepUID && ai == keepAcct {
				m.cursor = i
			}
		}
	} else if keepGroup != "" {
		for i, e := range m.visible() {
			if isGroupRow(e) && groupKeyOf(e) == keepGroup {
				m.cursor = i
			}
		}
	}
	m.clampCursor()
	if msg.folder == a.inbox() {
		m.saveInboxCache(msg.acct)
	}
	cmds = append(cmds, m.previewCmd())
	return tea.Batch(cmds...)
}

// previewCmd follows the highlighted mail: the reading pane shows it at once
// from the cache, otherwise the body is fetched in the background once the
// cursor has rested for previewDelay (so holding ↓ never queues fetches).
// Nothing here
// blocks; responses for a mail that is no longer highlighted are only cached.
func (m *Model) previewCmd() tea.Cmd {
	e, ok := m.selected()
	if !ok {
		if m.focus == focusReader {
			m.focus = focusList
		}
		m.pv = nil
		return nil
	}
	ai, fo := m.loc(e)
	k := bodyKey{ai, fo, e.UID}
	if m.pv != nil && m.pv.key == k {
		m.pv.env = e
		return nil // same mail (e.g. list refresh): timers are already running
	}
	m.selSeq++
	m.pv = &reader{key: k, env: e, inline: true}
	if m.focus == focusReader {
		m.focus = focusList
	}
	seq := m.selSeq
	var cmds []tea.Cmd
	if !m.bodies.Has(k) && !m.loading[k] {
		cmds = append(cmds, tea.Tick(previewDelay, func(time.Time) tea.Msg { return bodyLoadMsg{key: k, seq: seq} }))
	} else {
		cmds = append(cmds, m.prefetchVisible())
	}
	// (highlighting never marks a mail read: only opening it does)
	return tea.Batch(cmds...)
}

// selectionCurrent reports whether a timer armed for (key, seq) still
// refers to the highlighted mail.
func (m *Model) selectionCurrent(k bodyKey, seq int) bool {
	if seq != m.selSeq || m.pv == nil || m.pv.key != k {
		return false
	}
	e, ok := m.selected()
	ai, fo := m.loc(e)
	return ok && ai == k.acct && fo == k.folder && e.UID == k.uid
}

// prefetchVisible loads bodies of visible (and neighbouring) rows in the
// background, after the selected/opened mail. Prefetch uses the same
// FetchRaw path; on IMAP that is a dedicated connection, so it never
// holds up the opened body (which is requested first).
func (m *Model) prefetchVisible() tea.Cmd {
	v := m.visible()
	var cmds []tea.Cmd
	seen := map[bodyKey]bool{}
	add := func(i int) {
		if i < 0 || i >= len(v) || isGroupRow(v[i]) {
			return
		}
		ai, fo := m.loc(v[i])
		k := bodyKey{ai, fo, v[i].UID}
		if seen[k] {
			return
		}
		seen[k] = true
		cmds = append(cmds, m.loadBody(k))
	}
	add(m.cursor) // selected first (no-op if already cached or in flight)
	lo, hi := m.offset, min(len(v), m.offset+40)
	if lo < 0 {
		lo = 0
	}
	for i := lo; i < hi && len(cmds) < 16; i++ {
		add(i)
	}
	add(m.cursor + 1)
	add(m.cursor - 1)
	return tea.Batch(cmds...)
}

// enterReader moves focus into the reading pane; this counts as reading.
func (m *Model) enterReader(e backend.Envelope) tea.Cmd {
	ai, fo := m.loc(e)
	k := bodyKey{ai, fo, e.UID}
	if m.pv == nil || m.pv.key != k {
		hist, fromRead := []bodyKey(nil), m.readView
		if m.pv != nil && m.pv.inline {
			hist, fromRead = m.pv.hist, m.pv.fromRead
		}
		m.pv = &reader{key: k, env: e, inline: true, fromRead: fromRead, hist: hist}
	}
	m.focus = focusReader
	e.Seen = true
	m.pv.env = e
	cmd := m.markRead(ai, fo, e.UID)
	m.followOpened(e, true)
	return tea.Batch(m.loadBody(k), cmd)
}

// ---- moves: delete / archive / block ----

func (m *Model) moveCmd(ai int, folder string, e backend.Envelope, dest, what string, undoID int) tea.Cmd {
	return m.queueMove(ai, folder, e, dest, what, undoID)
}

func (m *Model) onMoveDone(d moveDoneMsg) tea.Cmd {
	if d.err != nil {
		if d.undoID != 0 {
			m.undo.Drop(d.undoID)
		}
		if d.what != "autoblock" {
			m.insertEnv(d.acct, d.folder, d.env)
		}
		m.toastErr("move", d.err)
		return nil
	}
	if d.undoID != 0 {
		m.undo.Confirm(d.undoID, d.newUID)
	}
	a := m.accts[d.acct]
	if d.what == "autoblock" {
		m.removeEnv(d.acct, d.folder, d.env.UID)
	}
	if a.Loaded[d.dest] {
		return m.loadList(d.acct, d.dest, false)
	}
	return nil
}

// deleteSelected moves the message to the trash (or asks before deleting
// for good when already in the trash).
func (m *Model) deleteMsg(ai int, folder string, e backend.Envelope) tea.Cmd {
	a := m.accts[ai]
	if f := a.folder(folder); f != nil && f.Role == backend.RoleTrash {
		m.confirm = &confirmBox{title: "Kalıcı olarak sil", text: "“" + trimSubj(e.Subject) + "” kalıcı olarak silinsin mi? Bu işlem geri alınamaz.",
			yes: "Sil", onYes: func() tea.Cmd {
				m.removeEnv(ai, folder, e.UID)
				m.toasts.MarkRead(ai, folder, e.UID)
				b := a.B
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
					defer cancel()
					err := b.Delete(ctx, folder, e.UID)
					if err != nil {
						return moveDoneMsg{acct: ai, folder: folder, env: e, err: err, what: "purge"}
					}
					return flagDoneMsg{}
				}
			}}
		return nil
	}
	trash := a.folderByRole(backend.RoleTrash)
	if trash == "" {
		m.toastInfo(toast.Error, "", "Çöp Kutusu bulunamadı.")
		return nil
	}
	m.removeEnv(ai, folder, e.UID)
	m.toasts.MarkRead(ai, folder, e.UID)
	id := m.undo.Push(undo.Entry{Op: undo.OpDelete, Account: ai, From: folder, Trash: trash, OrigUID: e.UID, MessageID: e.MessageID, Subject: e.Subject, Data: e})
	m.linkUndoToast(id, m.toasts.Push(toast.Deleted, "Silindi:", trimSubj(e.Subject), m.opt.Now(), 0))
	return tea.Batch(m.moveCmd(ai, folder, e, trash, "delete", id), m.previewCmd())
}

// purgeMails permanently deletes already-trashed mails (batched as one
// Delete per UID) and removes them from the list at once.
func (m *Model) purgeMails(mails []backend.Envelope) tea.Cmd {
	var cmds []tea.Cmd
	n := 0
	for _, e := range mails {
		ai, fo := m.loc(e)
		if ai >= len(m.accts) || m.accts[ai].B == nil {
			continue
		}
		a := m.accts[ai]
		m.removeEnv(ai, fo, e.UID)
		m.toasts.MarkRead(ai, fo, e.UID)
		b, uid, env, folder, acct := a.B, e.UID, e, fo, ai
		n++
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
			defer cancel()
			err := b.Delete(ctx, folder, uid)
			if err != nil {
				return moveDoneMsg{acct: acct, folder: folder, env: env, err: err, what: "purge"}
			}
			return flagDoneMsg{}
		})
	}
	m.clampCursor()
	if n > 0 {
		m.toasts.Push(toast.Deleted, "Silindi:", fmt.Sprintf("%d ileti kalıcı olarak silindi", n), m.opt.Now(), 0)
	}
	cmds = append(cmds, m.previewCmd())
	return tea.Batch(cmds...)
}

func trimSubj(s string) string {
	s = tidy(s)
	if s == "" {
		return "(konu yok)"
	}
	return s
}

// tidy makes a one-line display string: unsafe characters removed (see
// grid.Clean), runs of spaces collapsed, ends trimmed.
func tidy(s string) string {
	return strings.Join(strings.Fields(grid.Clean(s)), " ")
}

func (m *Model) undoLast() tea.Cmd {
	if m.bulkUndoable() {
		return m.undoBulk()
	}
	if top, ok := m.undo.Peek(); ok && top.Pending && top.Op == undo.OpCopy {
		m.dropUndo(top.ID)
		m.removeEnv(top.Account, top.Trash, placeholderUID(top.ID))
		if top.TrashUID != 0 {
			m.removeEnv(top.Account, top.Trash, top.TrashUID)
		}
		m.toastInfo(toast.Success, "", "Geri alındı")
		return m.previewCmd()
	}
	if top, ok := m.undo.Peek(); ok && top.Pending {
		// The server has not done it yet: cancel the queued move locally.
		if p, from, ok := m.cancelQueuedUndo(top.ID); ok {
			m.dropUndo(top.ID)
			if top.Op == undo.OpBlock && top.BlockAdded {
				m.opt.Blocks.Remove(top.BlockAccount, blocklist.Kind(top.BlockKind), top.BlockValue)
				m.saveBlocks()
			}
			m.insertEnv(top.Account, from, p.env)
			m.revealUndone(top.Account, from, p.env)
			m.toastInfo(toast.Success, "", "Geri alındı")
			return m.previewCmd()
		}
	}
	e, ok, pending := m.undo.Pop()
	if pending {
		m.toastInfo(toast.Info, "", "İşlem sürüyor, birazdan tekrar dene.")
		return nil
	}
	if !ok {
		m.toastInfo(toast.Info, "", "Geri alınacak bir şey yok.")
		return nil
	}
	m.dismissUndoToast(e.ID)
	if e.Account >= len(m.accts) {
		return nil
	}
	if e.Op == undo.OpFlag {
		return m.undoFlag(e)
	}
	if e.Op == undo.OpCopy {
		return m.undoCopy(e)
	}
	if e.Op == undo.OpBlock && e.BlockAdded {
		m.opt.Blocks.Remove(e.BlockAccount, blocklist.Kind(e.BlockKind), e.BlockValue)
		m.saveBlocks()
	}
	if env, ok := e.Data.(backend.Envelope); ok {
		m.insertEnv(e.Account, e.From, env)
		m.revealUndone(e.Account, e.From, env)
	}
	b := m.accts[e.Account].B
	dest := e.Trash
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
		defer cancel()
		uid := e.TrashUID
		if uid == 0 && e.MessageID != "" {
			res, err := b.Search(ctx, dest, e.MessageID, 5)
			if err == nil && len(res) > 0 {
				uid = res[0].UID
			}
		}
		if uid == 0 {
			return undoDoneMsg{e: e, err: fmtErr("ileti bulunamadı")}
		}
		newUID, err := b.Move(ctx, dest, uid, e.From, e.MessageID)
		return undoDoneMsg{e: e, err: err, newUID: newUID}
	}
}

func (m *Model) mailVisibleIndex(ai int, folder string, uid uint32) (int, bool) {
	for i, e := range m.visible() {
		if isGroupRow(e) {
			continue
		}
		a, f := m.loc(e)
		if a == ai && e.UID == uid && (f == folder || m.virt == viewSaved) {
			return i, true
		}
	}
	return 0, false
}

// revealUndone puts the list back on the undone mail and highlights it.
func (m *Model) revealUndone(ai int, folder string, env backend.Envelope) {
	if ai >= len(m.accts) {
		return
	}
	if _, ok := m.mailVisibleIndex(ai, folder, env.UID); !ok && !m.searching() {
		if folder == m.accts[ai].inbox() && env.Seen && m.virt == viewUnified {
			m.readView = true
		} else {
			m.virt = viewFolder
			m.cur, m.folder = ai, folder
			m.readView = folder == m.accts[ai].inbox() && env.Seen
			m.dropSearch()
			m.focus = focusList
		}
	}
	if i, ok := m.mailVisibleIndex(ai, folder, env.UID); ok {
		m.cursor = i
	}
	m.clampCursor()
}

func (m *Model) retargetUID(ai int, folder string, old, neu uint32) {
	if ai >= len(m.accts) || neu == 0 || neu == old {
		return
	}
	a := m.accts[ai]
	for i := range a.Lists[folder] {
		if a.Lists[folder][i].UID == old {
			a.Lists[folder][i].UID = neu
		}
	}
	for i := range a.Saved {
		if a.Saved[i].UID == old && (a.Saved[i].Folder == folder || a.Saved[i].Folder == "") {
			a.Saved[i].UID = neu
		}
	}
	if m.pv != nil && m.pv.key.acct == ai && m.pv.key.folder == folder && m.pv.key.uid == old {
		m.pv.key.uid = neu
		m.pv.env.UID = neu
	}
}

func (m *Model) undoFlag(e undo.Entry) tea.Cmd {
	if e.Account >= len(m.accts) {
		return nil
	}
	env, _ := e.Data.(backend.Envelope)
	if env.UID == 0 {
		env.UID = e.OrigUID
		env.MessageID = e.MessageID
		env.Subject = e.Subject
	}
	on := !e.FlagOn
	// If the original flag change is still queued, cancel it and just
	// restore the local value (the server never saw the change).
	if id, has := m.undoOps[e.ID]; has {
		if _, ok := m.ops.Cancel(id); ok {
			delete(m.opData, id)
			delete(m.undoOps, e.ID)
			if backend.Flag(e.Flag) == backend.FlagFlagged {
				m.setFlaggedLocal(e.Account, e.From, env, on)
			} else {
				m.setSeenLocal(e.Account, e.From, env.UID, on)
			}
			m.revealUndone(e.Account, e.From, env)
			m.toastInfo(toast.Success, "", "Geri alındı")
			return m.previewCmd()
		}
	}
	if backend.Flag(e.Flag) == backend.FlagFlagged {
		m.setFlaggedLocal(e.Account, e.From, env, on)
	} else {
		m.setSeenLocal(e.Account, e.From, env.UID, on)
	}
	m.revealUndone(e.Account, e.From, env)
	m.toastInfo(toast.Success, "", "Geri alındı")
	return tea.Batch(m.queueFlag(e.Account, e.From, env, backend.Flag(e.Flag), on), m.previewCmd())
}

func (m *Model) archiveMsg(ai int, folder string, e backend.Envelope) tea.Cmd {
	a := m.accts[ai]
	dest := a.folderByRole(backend.RoleArchive)
	if dest == "" {
		dest = a.folderByRole(backend.RoleAll)
	}
	if dest == "" || dest == folder {
		m.toastInfo(toast.Error, "", "Arşiv klasörü bulunamadı.")
		return nil
	}
	m.removeEnv(ai, folder, e.UID)
	m.toasts.MarkRead(ai, folder, e.UID)
	id := m.undo.Push(undo.Entry{Op: undo.OpMove, Account: ai, From: folder, Trash: dest, OrigUID: e.UID, MessageID: e.MessageID, Subject: e.Subject, Data: e})
	m.linkUndoToast(id, m.toasts.Push(toast.Deleted, "Arşivlendi:", trimSubj(e.Subject), m.opt.Now(), 0))
	return tea.Batch(m.moveCmd(ai, folder, e, dest, "archive", id), m.previewCmd())
}

// ---- blocking ----

func (m *Model) saveBlocks() {
	if err := m.opt.Blocks.Save(); err != nil {
		m.toastErr("blocklist", err)
	}
}

// ensureSpam finds the account's spam folder, creating "Spam" when missing,
// then runs then(name).
func (m *Model) ensureSpam(ai int, then func(name string) tea.Cmd) tea.Cmd {
	a := m.accts[ai]
	if n := a.folderByRole(backend.RoleJunk); n != "" {
		return then(n)
	}
	for _, f := range a.Folders {
		switch strings.ToLower(f.Name) {
		case "spam", "junk", "gereksiz", "[gmail]/spam", "junk e-mail", "gereksiz e-posta":
			return then(f.Name)
		}
	}
	b := a.B
	a.Folders = append(a.Folders, backend.Folder{Name: "Spam", Role: backend.RoleJunk})
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		err := b.CreateFolder(ctx, "Spam")
		return folderCreatedMsg{acct: ai, name: "Spam", err: err, then: func() tea.Cmd { return then("Spam") }}
	}
}

func (m *Model) blockSender(ai int, folder string, e backend.Envelope, kind blocklist.Kind) tea.Cmd {
	a := m.accts[ai]
	if e.FromAddr == "" || !strings.Contains(e.FromAddr, "@") {
		m.toastInfo(toast.Error, "", "Gönderen adresi okunamadı.")
		return nil
	}
	value := e.FromAddr
	if kind == blocklist.Domain {
		value = blocklist.BaseDomain(e.FromAddr)
	}
	entry, added := m.opt.Blocks.Add(a.Cfg.Email, kind, value, m.opt.Now())
	m.saveBlocks()
	if f := a.folder(folder); f != nil && f.Role == backend.RoleJunk {
		m.toastInfo(toast.Success, "Engellendi:", entry.Display())
		return nil
	}
	m.removeEnv(ai, folder, e.UID)
	m.toasts.MarkRead(ai, folder, e.UID)
	return m.ensureSpam(ai, func(spam string) tea.Cmd {
		id := m.undo.Push(undo.Entry{Op: undo.OpBlock, Account: ai, From: folder, Trash: spam, OrigUID: e.UID, MessageID: e.MessageID,
			Subject: e.Subject, Data: e, BlockAccount: a.Cfg.Email, BlockKind: string(kind), BlockValue: entry.Value, BlockAdded: added})
		m.linkUndoToast(id, m.toasts.Push(toast.Deleted, "Engellendi:", entry.Display(), m.opt.Now(), 0))
		return tea.Batch(m.moveCmd(ai, folder, e, spam, "block", id), m.previewCmd())
	})
}

// autoBlock silently moves a new message from a blocked sender to spam.
func (m *Model) autoBlock(ai int, folder string, e backend.Envelope) tea.Cmd {
	return m.ensureSpam(ai, func(spam string) tea.Cmd {
		return m.moveCmd(ai, folder, e, spam, "autoblock", 0)
	})
}

func (m *Model) dismissUndoToast(undoID int) {
	tid, ok := m.undoToasts[undoID]
	if !ok {
		return
	}
	delete(m.undoToasts, undoID)
	left := 0
	for _, id := range m.undoToasts {
		if id == tid {
			left++
		}
	}
	if left == 0 {
		m.toasts.Remove(tid)
		return
	}
	m.toasts.SetCount(tid, left, m.opt.Now())
}

func (m *Model) linkUndoToast(undoID, toastID int) {
	if m.undoToasts == nil {
		m.undoToasts = map[int]int{}
	}
	for k := range m.undoToasts { // keep the map small
		if k < undoID-20 {
			delete(m.undoToasts, k)
		}
	}
	m.undoToasts[undoID] = toastID
}

// termSizeFn is replaced in tests.
var termSizeFn = termSize

// applySize records the real window size and lays the UI out one column
// narrower (edgeMargin). A full clear follows so nothing stale survives.
func (m *Model) applySize(w, h int) tea.Cmd {
	if w <= 0 || h <= 0 {
		return nil
	}
	changed := w != m.realW || h != m.realH
	m.realW, m.realH = w, h
	m.w, m.h = max(w-edgeMargin, 40), max(h, 12)
	for _, t := range m.tabs {
		if t.compose != nil {
			t.compose.resize(m.w, m.h)
		}
	}
	if m.login != nil {
		m.login.resize(m.w)
	}
	if changed {
		return tea.ClearScreen
	}
	return nil
}

// dedupeFolders keeps the first folder of each name (servers or a refresh
// racing with a create can list a mailbox twice).
func dedupeFolders(in []backend.Folder) []backend.Folder {
	seen := make(map[string]bool, len(in))
	out := make([]backend.Folder, 0, len(in))
	for _, f := range in {
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		out = append(out, f)
	}
	return out
}

// dedupeEnvs keeps the first envelope of each UID.
func dedupeEnvs(in []backend.Envelope) []backend.Envelope {
	seen := make(map[uint32]bool, len(in))
	out := make([]backend.Envelope, 0, len(in))
	for _, e := range in {
		if seen[e.UID] {
			continue
		}
		seen[e.UID] = true
		out = append(out, e)
	}
	return out
}

// textKey: a printable character that arrives with Alt (AltGr on Windows,
// e.g. '@' = AltGr+Q on a Turkish keyboard, or a terminal that marks AltGr
// as Alt) is text, never a shortcut. Alt+letter / Alt+digit stay shortcuts.
func textKey(k tea.KeyMsg) tea.KeyMsg {
	if k.Type != tea.KeyRunes || !k.Alt || len(k.Runes) == 0 {
		return k
	}
	if len(k.Runes) == 1 {
		r := k.Runes[0]
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || !unicode.IsPrint(r) {
			return k
		}
		if r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return k
		}
	}
	k.Alt = false
	return k
}

func anyPrintable(rs []rune) bool {
	for _, r := range rs {
		if edit.Printable(r) {
			return true
		}
	}
	return false
}
