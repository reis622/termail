package ui

import (
	"context"
	"errors"
	"net"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/opqueue"
	"termail/internal/undo"
)

// opsWindow is how long quick consecutive changes of one mailbox are
// collected into one server command (several "d" presses -> one UID MOVE).
const opsWindow = 300 * time.Millisecond

type opsTickMsg struct{}

type opBatchMsg struct {
	b       *opqueue.Batch
	newUIDs map[uint32]uint32
	err     error
}

type movePayload struct {
	env         backend.Envelope
	undoID      int
	what        string
	placeholder uint32 // UID of the local copy shown in the destination
}

type flagPayload struct {
	env    backend.Envelope
	undoID int
}

var errNotConnected = errors.New("hesap bağlı değil")

// placeholderUID gives the optimistic copy in the destination folder a UID
// that cannot clash with real ones until the server reports the new UID.
func placeholderUID(opID int) uint32 { return 0xFFFFFFFF - uint32(opID%1_000_000) }

// queueMove applies nothing itself (callers already removed the row); it
// shows the mail in the destination's local list at once and queues the
// server move.
func (m *Model) queueMove(ai int, folder string, e backend.Envelope, dest, what string, undoID int) tea.Cmd {
	if ai < len(m.accts) && m.accts[ai].B == nil {
		m.holdOp(ai, opqueue.Op{Kind: opqueue.Move, Acct: ai, Folder: folder, UID: e.UID, Dest: dest})
		return nil
	}
	if ai >= len(m.accts) {
		return nil
	}
	p := movePayload{env: e, undoID: undoID, what: what}
	id := m.ops.Add(opqueue.Op{Kind: opqueue.Move, Acct: ai, Folder: folder, UID: e.UID, Dest: dest}, time.Now())
	p.placeholder = placeholderUID(id)
	m.opData[id] = p
	if undoID != 0 {
		m.undoOps[undoID] = id
	}
	// Always keep a local copy in the destination so opening Çöp Kutusu
	// mid-move shows the mails immediately (cached first), even if the
	// folder has never been listed.
	c := e
	c.UID = p.placeholder
	m.insertEnv(ai, dest, c)
	return m.scheduleOps()
}

// queueFlag queues a flag change (the caller already changed it locally).
func (m *Model) queueFlag(ai int, folder string, e backend.Envelope, f backend.Flag, on bool) tea.Cmd {
	if ai < len(m.accts) && m.accts[ai].B == nil {
		m.holdOp(ai, opqueue.Op{Kind: opqueue.Flag, Acct: ai, Folder: folder, UID: e.UID, Flag: f, On: on})
		return nil
	}
	if ai >= len(m.accts) {
		return nil
	}
	m.noteFlag(ai, folder, e, f, on)
	id := m.ops.Add(opqueue.Op{Kind: opqueue.Flag, Acct: ai, Folder: folder, UID: e.UID, Flag: f, On: on}, time.Now())
	m.opData[id] = flagPayload{env: e}
	return m.scheduleOps()
}

// queueFlagUndo records an already-applied local flag change so u can
// reverse it, then queues the server update.
func (m *Model) queueFlagUndo(ai int, folder string, e backend.Envelope, f backend.Flag, on bool) tea.Cmd {
	id := m.undo.Push(undo.Entry{Op: undo.OpFlag, Account: ai, From: folder, OrigUID: e.UID, MessageID: e.MessageID, Subject: e.Subject, Data: e, Flag: string(f), FlagOn: on})
	m.undo.Confirm(id, 0)
	if ai < len(m.accts) && m.accts[ai].B == nil {
		m.holdOp(ai, opqueue.Op{Kind: opqueue.Flag, Acct: ai, Folder: folder, UID: e.UID, Flag: f, On: on})
		return nil
	}
	if ai >= len(m.accts) {
		return nil
	}
	m.noteFlag(ai, folder, e, f, on)
	opid := m.ops.Add(opqueue.Op{Kind: opqueue.Flag, Acct: ai, Folder: folder, UID: e.UID, Flag: f, On: on}, time.Now())
	m.opData[opid] = flagPayload{env: e, undoID: id}
	m.undoOps[id] = opid
	return m.scheduleOps()
}

// scheduleOps starts every batch that is due and arms one timer for the next.
func (m *Model) scheduleOps() tea.Cmd {
	now := time.Now()
	var cmds []tea.Cmd
	for _, b := range m.ops.Ready(now) {
		cmds = append(cmds, m.runBatch(b))
	}
	if d, ok := m.ops.Next(now); ok && !m.opsTimer {
		m.opsTimer = true
		cmds = append(cmds, tea.Tick(max(d, time.Millisecond), func(time.Time) tea.Msg { return opsTickMsg{} }))
	}
	return tea.Batch(cmds...)
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// moveTimeout is long enough for a UID MOVE of n messages (Gmail in
// particular is slow on batches of a few hundred).
func moveTimeout(n int) time.Duration {
	d := 45*time.Second + time.Duration(n)*1500*time.Millisecond
	if d < 90*time.Second {
		d = 90 * time.Second
	}
	if d > 3*time.Minute {
		d = 3 * time.Minute
	}
	return d
}

func (m *Model) runBatch(b *opqueue.Batch) tea.Cmd {
	be := m.accts[b.Acct].B
	if be == nil {
		return func() tea.Msg { return opBatchMsg{b: b, err: errNotConnected} }
	}
	return func() tea.Msg {
		switch b.Kind {
		case opqueue.Move:
			nu, err := moveManyRetry(m.ctx, be, b)
			return opBatchMsg{b: b, newUIDs: nu, err: err}
		default:
			ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
			defer cancel()
			return opBatchMsg{b: b, err: be.SetFlagMany(ctx, b.Folder, b.UIDs(), b.Flag, b.On)}
		}
	}
}

func moveManyRetry(parent context.Context, be backend.Backend, b *opqueue.Batch) (map[uint32]uint32, error) {
	d := moveTimeout(len(b.Ops))
	var last error
	for i, to := range []time.Duration{d, d + time.Minute} {
		ctx, cancel := context.WithTimeout(parent, to)
		nu, err := be.MoveMany(ctx, b.Folder, b.UIDs(), b.Dest)
		cancel()
		if err == nil {
			return nu, nil
		}
		last = err
		if !isTimeout(err) || i == 1 || parent.Err() != nil {
			return nu, err
		}
	}
	return nil, last
}

// onOpBatch finishes a batch: confirm (new UIDs for undo, real UID for the
// destination copy) or roll every op of it back with one friendly toast.
func (m *Model) onOpBatch(msg opBatchMsg) tea.Cmd {
	b := msg.b
	m.ops.Done(b)
	// Changes replayed from the last run: nothing to roll back or report
	// locally (the server simply may have done them already).
	kept := b.Ops[:0:0]
	for _, o := range b.Ops {
		if _, ok := m.opData[o.ID].(replayPayload); ok {
			delete(m.opData, o.ID)
			if msg.err != nil {
				m.log.Printf("replay %v uid %d: %v", o.Kind, o.UID, msg.err)
			}
			continue
		}
		kept = append(kept, o)
	}
	if len(kept) == 0 {
		return m.scheduleOps()
	}
	b.Ops = kept
	var cmds []tea.Cmd
	if b.Acct < len(m.accts) {
		a := m.accts[b.Acct]
		switch b.Kind {
		case opqueue.Move:
			reload := false
			for i := len(b.Ops) - 1; i >= 0; i-- {
				o := b.Ops[i]
				p, _ := m.opData[o.ID].(movePayload)
				delete(m.opData, o.ID)
				if p.what == "bulk" {
					m.onBulkMoved(b.Acct, b.Folder, o.UID, msg.newUIDs[o.UID], msg.err)
				}
				if msg.err != nil {
					m.removeEnv(b.Acct, b.Dest, p.placeholder)
					if p.undoID != 0 {
						m.dropUndo(p.undoID)
					}
					if p.what != "autoblock" {
						m.insertEnv(b.Acct, b.Folder, p.env) // back at its place (lists are date-sorted)
					}
					continue
				}
				if p.undoID != 0 {
					m.undo.Confirm(p.undoID, msg.newUIDs[o.UID])
					delete(m.undoOps, p.undoID)
				}
				if p.what == "autoblock" {
					m.removeEnv(b.Acct, b.Folder, o.UID)
				}
				if nu := msg.newUIDs[o.UID]; nu != 0 {
					for j := range a.Lists[b.Dest] {
						if a.Lists[b.Dest][j].UID == p.placeholder {
							a.Lists[b.Dest][j].UID = nu
						}
					}
				} else {
					reload = true
				}
			}
			m.bulkProgress()
			if msg.err != nil {
				m.toastErr("move", msg.err)
			} else if reload && a.Loaded[b.Dest] && (m.bulk == nil || m.bulk.done+m.bulk.failed >= m.bulk.total) {
				// Don't restart a paged trash load after every chunk of a
				// bulk move; one refresh when the queue is done is enough.
				cmds = append(cmds, m.loadList(b.Acct, b.Dest, true))
			}
			cmds = append(cmds, m.refreshFolderStatus(b.Acct, b.Folder, b.Dest))
			m.clampCursor()
		case opqueue.Flag:
			for i, o := range b.Ops {
				p, _ := m.opData[o.ID].(flagPayload)
				delete(m.opData, o.ID)
				env := p.env
				if env.UID == 0 {
					env.UID = o.UID
				}
				m.flagSettled(b.Acct, b.Folder, env, b.Flag, msg.err)
				if msg.err == nil {
					delete(m.undoOps, p.undoID)
					continue
				}
				if p.undoID != 0 {
					m.dropUndo(p.undoID)
				}
				if i == 0 {
					m.onFlagDone(flagDoneMsg{err: msg.err, has: true, acct: b.Acct, folder: b.Folder, env: env, flag: b.Flag, on: b.On})
				} else if b.Flag == backend.FlagFlagged {
					m.setFlaggedLocal(b.Acct, b.Folder, env, !b.On)
				} else {
					m.setSeenLocal(b.Acct, b.Folder, o.UID, !b.On)
				}
			}
		}
	}
	return tea.Batch(append(cmds, m.scheduleOps())...)
}

// dropUndo forgets an undo entry and its "u geri al" hint.
func (m *Model) dropUndo(id int) {
	m.undo.Drop(id)
	delete(m.undoOps, id)
	m.dismissUndoToast(id)
}

// overlayPending makes a fresh server list agree with changes the server
// has not confirmed yet: moved mails stay hidden, local copies of mails
// moved here stay visible, pending flags keep the user's value.
func (m *Model) overlayPending(ai int, folder string, envs []backend.Envelope) []backend.Envelope {
	if m.ops.Pending() == 0 {
		return envs
	}
	out := make([]backend.Envelope, 0, len(envs))
	for _, e := range envs {
		if m.ops.Hidden(ai, folder, e.UID) {
			continue
		}
		if on, ok := m.ops.FlagState(ai, folder, e.UID, backend.FlagSeen); ok {
			e.Seen = on
		}
		if on, ok := m.ops.FlagState(ai, folder, e.UID, backend.FlagFlagged); ok {
			e.Flagged = on
		}
		out = append(out, e)
	}
	for _, o := range m.ops.MovesInto(ai, folder) {
		if p, ok := m.opData[o.ID].(movePayload); ok {
			c := p.env
			c.UID = p.placeholder
			out = append(out, c)
		}
	}
	sortEnv(out)
	return out
}

// cancelQueuedUndo undoes a change the server has not seen yet: the queued
// op is dropped and the mail goes back locally. ok=false when the op is
// already running (then the normal server-side undo applies later).
func (m *Model) cancelQueuedUndo(undoID int) (moveOp movePayload, folder string, ok bool) {
	id, has := m.undoOps[undoID]
	if !has {
		return movePayload{}, "", false
	}
	o, ok := m.ops.Cancel(id)
	if !ok {
		return movePayload{}, "", false
	}
	p, _ := m.opData[id].(movePayload)
	delete(m.opData, id)
	delete(m.undoOps, undoID)
	m.removeEnv(o.Acct, o.Dest, p.placeholder)
	return p, o.Folder, true
}

type folderStatusItem struct {
	name          string
	total, unread int
	err           error
}

type folderStatusMsg struct {
	acct  int
	items []folderStatusItem
}

// refreshFolderStatus STATUSes the folders a move just touched so the
// folder pane and account bar match the server, then reconciles any
// moves that are still queued.
func (m *Model) refreshFolderStatus(ai int, folders ...string) tea.Cmd {
	if ai >= len(m.accts) || m.accts[ai].B == nil {
		return nil
	}
	s, ok := m.accts[ai].B.(backend.FolderStatuser)
	if !ok {
		return func() tea.Msg { return folderStatusMsg{acct: ai} }
	}
	var names []string
	seen := map[string]bool{}
	for _, n := range folders {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	if len(names) == 0 {
		return func() tea.Msg { return folderStatusMsg{acct: ai} }
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
		defer cancel()
		items := make([]folderStatusItem, 0, len(names))
		for _, n := range names {
			t, u, err := s.FolderStatus(ctx, n)
			items = append(items, folderStatusItem{name: n, total: t, unread: u, err: err})
		}
		return folderStatusMsg{acct: ai, items: items}
	}
}

func (m *Model) onFolderStatus(msg folderStatusMsg) tea.Cmd {
	if msg.acct >= len(m.accts) {
		return nil
	}
	for _, it := range msg.items {
		if it.err != nil {
			m.logErr("status", it.err)
			continue
		}
		if f := m.accts[msg.acct].folder(it.name); f != nil {
			f.Total, f.Unread = it.total, it.unread
		}
	}
	m.reconcileFolderCounts(msg.acct)
	return nil
}

func (m *Model) opsMovesFrom(ai int, folder string) bool {
	for _, o := range m.ops.All() {
		if o.Acct == ai && o.Kind == opqueue.Move && o.Folder == folder {
			return true
		}
	}
	return false
}

// reconcileFolderCounts applies in-flight moves to server counts and, when
// the unread inbox list is complete, will not let a stale STATUS raise the
// Gelen Kutusu badge again.
func (m *Model) reconcileFolderCounts(ai int) {
	if ai >= len(m.accts) {
		return
	}
	a := m.accts[ai]
	type delta struct{ total, unread int }
	outg, inc := map[string]delta{}, map[string]delta{}
	for _, o := range m.ops.All() {
		if o.Acct != ai || o.Kind != opqueue.Move {
			continue
		}
		unseen := false
		if p, ok := m.opData[o.ID].(movePayload); ok {
			unseen = !p.env.Seen
		}
		d := outg[o.Folder]
		d.total++
		if unseen {
			d.unread++
		}
		outg[o.Folder] = d
		e := inc[o.Dest]
		e.total++
		if unseen {
			e.unread++
		}
		inc[o.Dest] = e
	}
	for i := range a.Folders {
		f := &a.Folders[i]
		if d, ok := outg[f.Name]; ok {
			f.Total = max(0, f.Total-d.total)
			f.Unread = max(0, f.Unread-d.unread)
		}
		if d, ok := inc[f.Name]; ok {
			f.Total += d.total
			f.Unread += d.unread
		}
	}
	inbox := a.inbox()
	if !a.Loaded[inbox] || m.opsMovesFrom(ai, inbox) {
		return
	}
	local := 0
	for _, e := range a.Lists[inbox] {
		if !e.Seen {
			local++
		}
	}
	f := a.folder(inbox)
	if f == nil {
		return
	}
	if local < f.Unread && (local == 0 || a.expectOf(inbox, "unseen") <= local) {
		f.Unread = local
	}
}
