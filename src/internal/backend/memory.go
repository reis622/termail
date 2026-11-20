package backend

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"termail/internal/mailmsg"
)

// Memory is an in-memory backend used by demo mode and UI tests.
type Memory struct {
	mu      sync.Mutex
	folders []Folder
	msgs    map[string][]memMsg
	nextUID map[string]uint32
	watch   []chan Event
	Sent    [][]byte // raw messages "sent"

	failFlags bool
	failMoves bool
	// Calls counts server operations by name (tests check batching).
	Calls map[string]int
	// Delay simulates a slow server for change operations.
	Delay time.Duration
	// FailSend makes Send fail with this error (tests).
	FailSend error
	// SearchDelay simulates a slow server search (tests, demo).
	SearchDelay time.Duration

	uidValidity map[string]uint32
	modSeq      map[string]uint64
	// DisableCondstore makes SyncFolder use the no-CONDSTORE fallback
	// (Outlook-style: UID SEARCH + flag FETCH, no CHANGEDSINCE).
	DisableCondstore bool
}

type memMsg struct {
	env Envelope
	raw []byte
}

// NewMemory creates an empty backend with the given folders.
func NewMemory(folders []Folder) *Memory {
	m := &Memory{msgs: map[string][]memMsg{}, nextUID: map[string]uint32{}, uidValidity: map[string]uint32{}, modSeq: map[string]uint64{}}
	m.folders = append(m.folders, folders...)
	return m
}

// Add stores a message (raw is parsed for the envelope fields if env is zero).
func (m *Memory) Add(folder string, env Envelope, raw []byte) uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addLocked(folder, env, raw)
}

func (m *Memory) addLocked(folder string, env Envelope, raw []byte) uint32 {
	if env.Subject == "" && env.From == "" && raw != nil {
		if p, err := mailmsg.Parse(raw); err == nil {
			env.Subject = p.Subject
			if len(p.From) > 0 {
				env.From = mailmsg.Display(p.From[0])
				env.FromAddr = p.From[0].Address
			}
			env.Date = p.Date
			env.MessageID = p.MessageID
			env.Attach = len(p.Attachments) > 0
		}
	}
	m.nextUID[folder]++
	env.UID = m.nextUID[folder]
	m.msgs[folder] = append(m.msgs[folder], memMsg{env, raw})
	m.touchLocked(folder)
	return env.UID
}

func (m *Memory) touchLocked(folder string) {
	if m.modSeq == nil {
		m.modSeq = map[string]uint64{}
	}
	if m.uidValidity == nil {
		m.uidValidity = map[string]uint32{}
	}
	if m.uidValidity[folder] == 0 {
		m.uidValidity[folder] = 1
	}
	m.modSeq[folder]++
}

// Deliver adds a message to a folder and notifies watchers (simulated push).
func (m *Memory) Deliver(folder string, env Envelope, raw []byte) uint32 {
	uid := m.Add(folder, env, raw)
	m.mu.Lock()
	ws := append([]chan Event(nil), m.watch...)
	m.mu.Unlock()
	for _, w := range ws {
		select {
		case w <- Event{Kind: EvNewMail, Folder: folder}:
		default:
		}
	}
	return uid
}

func (m *Memory) Connect(ctx context.Context) error { return nil }

func (m *Memory) Folders(ctx context.Context) ([]Folder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]Folder(nil), m.folders...)
	for i := range out {
		out[i].Unread, out[i].Total = 0, len(m.msgs[out[i].Name])
		for _, x := range m.msgs[out[i].Name] {
			if !x.env.Seen {
				out[i].Unread++
			}
		}
	}
	return out, nil
}

func (m *Memory) FolderStatus(ctx context.Context, folder string) (total, unread int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.msgs[folder] {
		total++
		if !x.env.Seen {
			unread++
		}
	}
	return total, unread, nil
}

func (m *Memory) List(ctx context.Context, folder string, limit int) ([]Envelope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Envelope
	for _, x := range m.msgs[folder] {
		out = append(out, x.env)
	}
	sortNewest(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListPage returns a newest-first window and the mailbox size.
func (m *Memory) ListPage(ctx context.Context, folder string, offset, limit int) ([]Envelope, int, error) {
	all, err := m.List(ctx, folder, 0)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	if offset >= total || limit == 0 {
		return nil, total, nil
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	return all[offset:end], total, nil
}

// ListFlagPage returns a newest-first window of unseen or seen messages.
func (m *Memory) ListFlagPage(ctx context.Context, folder string, unseen bool, offset, limit int) ([]Envelope, int, error) {
	all, err := m.List(ctx, folder, 0)
	if err != nil {
		return nil, 0, err
	}
	var out []Envelope
	for _, e := range all {
		if e.Seen != unseen {
			out = append(out, e)
		}
	}
	total := len(out)
	if offset >= total || limit == 0 {
		return nil, total, nil
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	return out[offset:end], total, nil
}

func (m *Memory) find(folder string, uid uint32) (int, error) {
	for i, x := range m.msgs[folder] {
		if x.env.UID == uid {
			return i, nil
		}
	}
	return -1, errors.New("ileti bulunamadı")
}

func (m *Memory) FetchRaw(ctx context.Context, folder string, uid uint32) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.find(folder, uid)
	if err != nil {
		return nil, err
	}
	return m.msgs[folder][i].raw, nil
}

func (m *Memory) SetFlag(ctx context.Context, folder string, uid uint32, f Flag, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failFlags {
		return errors.New("sunucu işlemi reddetti")
	}
	i, err := m.find(folder, uid)
	if err != nil {
		return err
	}
	switch f {
	case FlagSeen:
		m.msgs[folder][i].env.Seen = on
	case FlagFlagged:
		m.msgs[folder][i].env.Flagged = on
	}
	m.touchLocked(folder)
	return nil
}

func (m *Memory) Move(ctx context.Context, folder string, uid uint32, dest, messageID string) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.find(folder, uid)
	if err != nil {
		return 0, err
	}
	x := m.msgs[folder][i]
	m.msgs[folder] = append(m.msgs[folder][:i], m.msgs[folder][i+1:]...)
	m.touchLocked(folder)
	return m.addLocked(dest, x.env, x.raw), nil
}

// MoveMany implements Backend.
func (m *Memory) MoveMany(ctx context.Context, folder string, uids []uint32, dest string) (map[uint32]uint32, error) {
	m.slow(ctx, "MoveMany")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failMoves {
		return nil, errors.New("sunucu işlemi reddetti")
	}
	out := map[uint32]uint32{}
	for _, u := range uids {
		i, err := m.find(folder, u)
		if err != nil {
			return out, err
		}
		x := m.msgs[folder][i]
		m.msgs[folder] = append(m.msgs[folder][:i], m.msgs[folder][i+1:]...)
		m.touchLocked(folder)
		out[u] = m.addLocked(dest, x.env, x.raw)
	}
	return out, nil
}

// SetFlagMany implements Backend.
func (m *Memory) SetFlagMany(ctx context.Context, folder string, uids []uint32, f Flag, on bool) error {
	m.slow(ctx, "SetFlagMany")
	for _, u := range uids {
		if err := m.SetFlag(ctx, folder, u, f, on); err != nil {
			return err
		}
	}
	return nil
}

// FailMoves makes moves fail (tests).
func (m *Memory) FailMoves(fail bool) { m.mu.Lock(); m.failMoves = fail; m.mu.Unlock() }

func (m *Memory) slow(ctx context.Context, op string) {
	m.mu.Lock()
	if m.Calls == nil {
		m.Calls = map[string]int{}
	}
	m.Calls[op]++
	d := m.Delay
	m.mu.Unlock()
	if d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
	}
}

// CallCount returns how often op ran.
func (m *Memory) CallCount(op string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Calls[op]
}

func (m *Memory) Delete(ctx context.Context, folder string, uid uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.find(folder, uid)
	if err != nil {
		return err
	}
	m.msgs[folder] = append(m.msgs[folder][:i], m.msgs[folder][i+1:]...)
	m.touchLocked(folder)
	return nil
}

// Count implements Backend.
func (m *Memory) Count(ctx context.Context, folder string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.msgs[folder]; !ok && !m.hasFolder(folder) {
		return 0, errors.New("klasör bulunamadı")
	}
	return len(m.msgs[folder]), nil
}

// Empty implements Backend.
func (m *Memory) Empty(ctx context.Context, folder string, progress func(done, total int)) error {
	m.mu.Lock()
	var uids []uint32
	for _, x := range m.msgs[folder] {
		uids = append(uids, x.env.UID)
	}
	m.mu.Unlock()
	done := 0
	for _, b := range Batches(uids, EmptyBatch) {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.mu.Lock()
		gone := map[uint32]bool{}
		for _, u := range b {
			gone[u] = true
		}
		var keep []memMsg
		for _, x := range m.msgs[folder] {
			if !gone[x.env.UID] {
				keep = append(keep, x)
			}
		}
		m.msgs[folder] = keep
		m.touchLocked(folder)
		m.mu.Unlock()
		done += len(b)
		if progress != nil {
			progress(done, len(uids))
		}
	}
	return nil
}

func (m *Memory) Append(ctx context.Context, folder string, raw []byte, flags []Flag) error {
	env := Envelope{}
	m.mu.Lock()
	uid := m.addLocked(folder, env, raw)
	i, _ := m.find(folder, uid)
	for _, f := range flags {
		if f == FlagSeen {
			m.msgs[folder][i].env.Seen = true
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *Memory) Search(ctx context.Context, folder, query string, limit int) ([]Envelope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := strings.ToLower(query)
	var out []Envelope
	for _, x := range m.msgs[folder] {
		if strings.Contains(strings.ToLower(x.env.Subject+" "+x.env.From+" "+x.env.FromAddr+" "+string(x.raw)), q) {
			out = append(out, x.env)
		}
	}
	sortNewest(out)
	return out, nil
}

func (m *Memory) Flagged(ctx context.Context, folder string, limit int) ([]Envelope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Envelope
	virtual := false // like Gmail's "Starred": a view over all folders
	for _, f := range m.folders {
		if f.Name == folder && f.Role == RoleFlagged {
			virtual = true
		}
	}
	for name, l := range m.msgs {
		if name != folder && !virtual {
			continue
		}
		for _, x := range l {
			if x.env.Flagged {
				e := x.env
				if virtual {
					e.Folder = name // where the message really lives
				}
				out = append(out, e)
			}
		}
	}
	sortNewest(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// FailFlags makes SetFlag fail (tests of the optimistic rollback).
func (m *Memory) FailFlags(fail bool) {
	m.mu.Lock()
	m.failFlags = fail
	m.mu.Unlock()
}

// Prewarm counts calls (tests).
func (m *Memory) Prewarm(ctx context.Context) error {
	m.slow(ctx, "Prewarm")
	return nil
}

func (m *Memory) Send(ctx context.Context, from string, rcpts []string, raw []byte, sentFolder string) error {
	m.slow(ctx, "Send")
	m.mu.Lock()
	if m.FailSend != nil {
		err := m.FailSend
		m.mu.Unlock()
		return err
	}
	m.Sent = append(m.Sent, raw)
	m.mu.Unlock()
	if sentFolder != "" {
		return m.Append(ctx, sentFolder, raw, []Flag{FlagSeen})
	}
	return nil
}

func (m *Memory) Watch(ctx context.Context, inbox string, notify func(Event)) {
	ch := make(chan Event, 16)
	m.mu.Lock()
	m.watch = append(m.watch, ch)
	m.mu.Unlock()
	notify(Event{Kind: EvOnline, Folder: inbox})
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-ch:
			if e.Folder == inbox {
				notify(e)
			}
		}
	}
}

func (m *Memory) Close() error { return nil }

func (m *Memory) CreateFolder(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.folders {
		if f.Name == name {
			return errors.New("klasör zaten var")
		}
	}
	m.folders = append(m.folders, Folder{Name: name, Role: RoleByName(name)})
	return nil
}

func (m *Memory) Copy(ctx context.Context, folder string, uid uint32, dest, messageID string) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.find(folder, uid)
	if err != nil {
		return 0, err
	}
	x := m.msgs[folder][i]
	raw := append([]byte(nil), x.raw...)
	return m.addLocked(dest, x.env, raw), nil
}

func (m *Memory) DeleteFolder(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasFolder(name) {
		return errors.New("klasör bulunamadı")
	}
	var keep []Folder
	for _, f := range m.folders {
		if f.Name == name || strings.HasPrefix(f.Name, name+"/") || strings.HasPrefix(f.Name, name+".") {
			delete(m.msgs, f.Name)
			continue
		}
		keep = append(keep, f)
	}
	m.folders = keep
	delete(m.msgs, name)
	return nil
}

func (m *Memory) RenameFolder(ctx context.Context, oldName, newName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasFolder(oldName) {
		return errors.New("klasör bulunamadı")
	}
	if m.hasFolder(newName) {
		return errors.New("klasör zaten var")
	}
	for i, f := range m.folders {
		if f.Name == oldName || strings.HasPrefix(f.Name, oldName+"/") || strings.HasPrefix(f.Name, oldName+".") {
			f.Name = newName + f.Name[len(oldName):]
			f.Role = RoleByName(f.Name)
			m.folders[i] = f
		}
	}
	renamed := map[string][]memMsg{}
	for name, msgs := range m.msgs {
		if name == oldName || strings.HasPrefix(name, oldName+"/") || strings.HasPrefix(name, oldName+".") {
			renamed[newName+name[len(oldName):]] = msgs
			delete(m.msgs, name)
		}
	}
	for name, msgs := range renamed {
		m.msgs[name] = msgs
		m.touchLocked(name)
	}
	return nil
}

func (m *Memory) hasFolder(name string) bool {
	for _, f := range m.folders {
		if f.Name == name {
			return true
		}
	}
	return false
}
