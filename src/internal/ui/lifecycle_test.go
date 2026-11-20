package ui

import (
	"bytes"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/discover"
	"termail/internal/localstore"
)

// screen is a fake terminal that remembers when the first frame arrived.
type screen struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	first chan struct{}
	once  sync.Once
}

func newScreen() *screen { return &screen{first: make(chan struct{})} }

func (s *screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.buf.Write(p)
	s.mu.Unlock()
	if bytes.Contains(p, []byte("Termail")) {
		s.once.Do(func() { close(s.first) })
	}
	return len(p), nil
}

func (s *screen) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ansi.Strip(s.buf.String())
}

func (s *screen) waitFor(t *testing.T, what string, d time.Duration) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if strings.Contains(s.text(), what) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%q never appeared on screen", what)
}

// stalled accepts connections and never answers.
func stalled(t *testing.T) discover.Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(p)
	return discover.Server{Host: h, Port: n, Security: discover.None}
}

func cachedStore(t *testing.T, email string) *localstore.Store {
	t.Helper()
	st := &localstore.Store{Dir: t.TempDir()}
	now := time.Now()
	in := localstore.Inbox{Email: email, Folder: "INBOX", Saved: now,
		Folders: []localstore.Folder{{Name: "INBOX", Role: string(backend.RoleInbox)}, {Name: "Trash", Role: string(backend.RoleTrash)}}}
	for i := 0; i < 30; i++ {
		in.List = append(in.List, localstore.Header{UID: uint32(100 - i), From: "Gönderen " + strconv.Itoa(i), Subject: "Önbellekten konu " + strconv.Itoa(i), Date: now.Add(-time.Duration(i) * time.Hour)})
	}
	if err := st.SaveInbox(in); err != nil {
		t.Fatal(err)
	}
	return st
}

// The first frame appears before any account work: the sign-in read is slow,
// the server never answers, and the cached inbox is on screen anyway. Then
// q leaves at once, the stuck connections notwithstanding, and the change
// the server never confirmed is kept for the next start.
func TestFirstFrameAndExitWithStalledServer(t *testing.T) {
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	email := "ahmet@test.local"
	st := cachedStore(t, email)
	srv := stalled(t)
	built := make(chan struct{})
	a := &Acct{Cfg: account.Account{Name: "Test", Email: email}, Build: func() (backend.Backend, error) {
		time.Sleep(300 * time.Millisecond) // a slow Credential Manager
		defer close(built)
		return backend.NewIMAP(backend.Config{Incoming: srv, Outgoing: srv, Username: email, Password: "x"}), nil
	}}
	m := New(Options{Accounts: []*Acct{a}, Store: st})
	scr := newScreen()
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(scr), tea.WithoutSignalHandler())
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- m.run(p) }()
	select {
	case <-scr.first:
	case <-time.After(2 * time.Second):
		t.Fatal("no first frame")
	}
	ttf := time.Since(start)
	t.Logf("first frame after %v (sign-in read takes 300 ms, server never answers)", ttf)
	if ttf > 100*time.Millisecond {
		t.Fatalf("first frame took %v", ttf)
	}
	scr.waitFor(t, "Önbellekten konu 0", time.Second) // the last run's list, before any server reply
	scr.waitFor(t, "Bağlanıyor", time.Second)
	<-built
	time.Sleep(100 * time.Millisecond)                         // reading + push connections now stuck on the server
	p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}) // delete the top mail: the server never confirms
	time.Sleep(450 * time.Millisecond)                         // (past the coalescing window: the move is running, stuck)
	t0 := time.Now()
	p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("q did not leave")
	}
	exit := time.Since(t0)
	t.Logf("q → program ended (terminal restored, connections dropped): %v", exit)
	if exit > 300*time.Millisecond {
		t.Fatalf("exit took %v", exit)
	}
	ops := st.TakeOps()
	if len(ops) != 1 || ops[0].Kind != "move" || ops[0].UID != 100 || ops[0].Dest != "Trash" {
		t.Fatalf("pending change not kept: %+v", ops)
	}
}

// Changes kept by a fast exit are sent on the next start, and the cached
// list already shows them.
func TestPendingOpsReplayedOnNextStart(t *testing.T) {
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	email := "ahmet@test.local"
	st := cachedStore(t, email)
	mem := backend.NewMemory([]backend.Folder{{Name: "INBOX", Role: backend.RoleInbox}, {Name: "Trash", Role: backend.RoleTrash}})
	for i := 0; i < 30; i++ {
		mem.Add("INBOX", backend.Envelope{From: "x", Subject: "s" + strconv.Itoa(i), Date: time.Now().Add(-time.Duration(30-i) * time.Hour)}, []byte("Subject: s\r\n\r\nx"))
	}
	// the memory backend numbers 1..30; make the stored ops point at real ones
	st.SaveOps([]localstore.Op{{Email: email, Kind: "move", Folder: "INBOX", UID: 30, Dest: "Trash"}, {Email: email, Kind: "flag", Folder: "INBOX", UID: 29, Flag: string(backend.FlagSeen), On: true}})
	connected := make(chan struct{})
	a := &Acct{Cfg: account.Account{Name: "Test", Email: email}, Build: func() (backend.Backend, error) {
		<-connected
		return mem, nil
	}}
	m := New(Options{Accounts: []*Acct{a}, Store: st})
	m.applySize(140, 40)
	m.Update(m.loadLocal()())
	if len(m.replay) != 2 {
		t.Fatalf("replay %+v", m.replay)
	}
	// still connecting: a quick exit now keeps them again
	m.persistForExit()
	if again := st.TakeOps(); len(again) != 2 {
		t.Fatalf("ops lost while connecting: %+v", again)
	}
	m.persisted = false
	close(connected)
	m.Update(builtMsg{acct: 0, b: mem})
	drainOps(t, m)
	if n, _ := mem.Count(t.Context(), "Trash"); n != 1 {
		t.Fatalf("move not replayed: Trash has %d", n)
	}
	l, _ := mem.List(t.Context(), "INBOX", 100)
	for _, e := range l {
		if e.UID == 29 && !e.Seen {
			t.Fatal("flag not replayed")
		}
		if e.UID == 30 {
			t.Fatal("moved mail still in INBOX")
		}
	}
	if len(m.replay) != 0 || m.ops.Pending() != 0 {
		t.Fatal("replay not finished")
	}
	m.persisted = false
	m.persistForExit()
	if left := st.TakeOps(); len(left) != 0 {
		t.Fatalf("finished ops stored again: %+v", left)
	}
}

// Changes made on the cached list before the account is up are not lost.
func TestChangesWhileConnectingAreKept(t *testing.T) {
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	email := "ahmet@test.local"
	st := cachedStore(t, email)
	a := &Acct{Cfg: account.Account{Name: "Test", Email: email}, Build: func() (backend.Backend, error) { select {} }}
	m := New(Options{Accounts: []*Acct{a}, Store: st})
	m.applySize(140, 40)
	m.startAccount(0) // (its build never finishes)
	m.Update(m.loadLocal()())
	if !strings.Contains(strings.Join(frameLines(m), "\n"), "Önbellekten konu 0") {
		t.Fatal("cached list not shown")
	}
	press(m, "d")
	press(m, "i")
	m.exitNow()
	ops := st.TakeOps()
	if len(ops) != 2 || ops[0].Kind != "move" || ops[1].Kind != "flag" {
		t.Fatalf("%+v", ops)
	}
}

// Closing the window: an open, unsent message is kept and reopened next time.
func TestWindowCloseKeepsUnsentMessage(t *testing.T) {
	m, _, _ := newCompose(t)
	st := &localstore.Store{Dir: t.TempDir()}
	m.opt.Store = st
	done := make(chan struct{})
	m.Update(closeMsg{done: done})
	select {
	case <-done:
	default:
		t.Fatal("close hand-over not acknowledged")
	}
	m2, _ := memModel(t, 1)
	m2.opt.Store = st
	m2.Update(m2.loadLocal()())
	var re *composer
	for _, tb := range m2.tabs {
		if tb.compose != nil {
			re = tb.compose
		}
	}
	if re == nil || re.to.Value() != "arthur@gmail.com" || re.subject.Value() != "bak bura" || !strings.Contains(re.body.Value(), "şuna bir bak") {
		t.Fatal("unsent message not reopened")
	}
	if m2.active != 0 || !hasToast(m2, "yarım kalan 1 ileti") {
		t.Fatal("reopened message should wait in its tab with a note")
	}
}

// A corrupt cache never stops the start: the list simply loads from the server.
func TestCorruptCacheDoesNotBreakStart(t *testing.T) {
	m, _ := memModel(t, 2)
	st := &localstore.Store{Dir: t.TempDir()}
	st.SaveInbox(localstore.Inbox{Email: "ahmet@test.local", List: []localstore.Header{{UID: 1}}})
	// overwrite every file under the store with garbage
	m.opt.Store = st
	files, _ := filepathGlob(st.Dir)
	for _, f := range files {
		writeFile(f, "\x00garbage{")
	}
	m.Update(m.loadLocal()())
	checkFrame(t, "corrupt cache", m)
}

func filepathGlob(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

func writeFile(p, s string) { _ = os.WriteFile(p, []byte(s), 0o600) }
