package ui

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/emersion/go-message/mail"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/discover"
	"termail/internal/mailmsg"
	"termail/internal/testserver"
	"termail/internal/toast"
)

// opsModel: one account backed by b with mails 1..n in INBOX (newest = n).
func opsModel(t *testing.T, b backend.Backend, add func(subj string, d time.Time) uint32, n int) *Model {
	t.Helper()
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	now := time.Now()
	for i := 1; i <= n; i++ {
		add("ileti "+strconv.Itoa(i), now.Add(time.Duration(i-n)*time.Minute))
	}
	ctx := context.Background()
	fs, err := b.Folders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a := &Acct{Cfg: account.Account{Name: "Test", Email: "ahmet@test.local"}, B: b, Folders: fs}
	m := New(Options{Accounts: []*Acct{a}})
	m.applySize(140, 40)
	for _, f := range []string{"INBOX", a.folderByRole(backend.RoleTrash)} {
		l, err := b.List(ctx, f, 100)
		if err != nil {
			t.Fatal(err)
		}
		a.Lists[f], a.Loaded[f] = l, true
	}
	m.folder = "INBOX"
	return m
}

func memModel(t *testing.T, n int) (*Model, *backend.Memory) {
	mem := backend.NewMemory([]backend.Folder{{Name: "INBOX", Role: backend.RoleInbox}, {Name: "Trash", Role: backend.RoleTrash}, {Name: "Spam", Role: backend.RoleJunk}})
	m := opsModel(t, mem, func(s string, d time.Time) uint32 {
		return mem.Add("INBOX", backend.Envelope{From: "A", Subject: s, Date: d, MessageID: "<" + s + "@x>"}, []byte("Subject: "+s+"\r\n\r\nx"))
	}, n)
	return m, mem
}

func subjects(l []backend.Envelope) []string {
	var out []string
	for _, e := range l {
		out = append(out, e.Subject)
	}
	return out
}

func serverCount(t *testing.T, b backend.Backend, f string) int {
	n, err := b.Count(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestQuickDeletesBecomeOneServerMove(t *testing.T) {
	m, mem := memModel(t, 5)
	for i := 0; i < 3; i++ {
		m.onKey(key("d"))
	}
	a := m.accts[0]
	if len(a.Lists["INBOX"]) != 2 || len(a.Lists["Trash"]) != 3 {
		t.Fatalf("not instant: inbox %v trash %v", subjects(a.Lists["INBOX"]), subjects(a.Lists["Trash"]))
	}
	if serverCount(t, mem, "INBOX") != 5 || mem.CallCount("MoveMany") != 0 {
		t.Fatal("server touched before the coalescing window")
	}
	drainOps(t, m)
	if c := mem.CallCount("MoveMany"); c != 1 {
		t.Fatalf("%d server moves, want 1 batched", c)
	}
	if serverCount(t, mem, "INBOX") != 2 || serverCount(t, mem, "Trash") != 3 {
		t.Fatal("server state wrong")
	}
	for _, e := range a.Lists["Trash"] {
		if e.UID > 0xFFF00000 {
			t.Fatalf("placeholder UID left in trash: %+v", e)
		}
	}
}

func TestFailedMoveRollsBackToItsPlace(t *testing.T) {
	m, mem := memModel(t, 4)
	mem.FailMoves(true)
	m.cursor = 1
	before := subjects(m.accts[0].Lists["INBOX"])
	m.onKey(key("d"))
	if len(m.accts[0].Lists["INBOX"]) != 3 || len(m.accts[0].Lists["Trash"]) != 1 {
		t.Fatal("delete not applied at once")
	}
	drainOps(t, m)
	after := subjects(m.accts[0].Lists["INBOX"])
	if len(after) != len(before) || after[1] != before[1] {
		t.Fatalf("not restored at its position: %v -> %v", before, after)
	}
	if len(m.accts[0].Lists["Trash"]) != 0 {
		t.Fatal("local trash copy not removed")
	}
	found := false
	for _, x := range m.toasts.Actions() {
		found = found || x.Kind == toast.Error
	}
	if !found {
		t.Fatal("no error toast")
	}
}

func TestUndoBeforeServerCancelsLocally(t *testing.T) {
	m, mem := memModel(t, 3)
	m.onKey(key("d"))
	m.onKey(key("u"))
	if len(m.accts[0].Lists["INBOX"]) != 3 || len(m.accts[0].Lists["Trash"]) != 0 {
		t.Fatal("undo did not restore locally")
	}
	drainOps(t, m)
	if mem.CallCount("MoveMany") != 0 || serverCount(t, mem, "INBOX") != 3 {
		t.Fatal("cancelled move still reached the server")
	}
}

func TestUndoAfterServerMovesBack(t *testing.T) {
	m, mem := memModel(t, 3)
	m.onKey(key("d"))
	drainOps(t, m)
	if serverCount(t, mem, "Trash") != 1 {
		t.Fatal("move not done")
	}
	cmd := m.onKey(key("u"))
	if len(m.accts[0].Lists["INBOX"]) != 3 {
		t.Fatal("undo not shown at once")
	}
	if cmd == nil {
		t.Fatal("no server undo")
	}
	m.Update(cmd())
	if serverCount(t, mem, "INBOX") != 3 || serverCount(t, mem, "Trash") != 0 {
		t.Fatal("server undo failed")
	}
}

func TestOpsKeepOrderAndOverlayStaleLists(t *testing.T) {
	m, mem := memModel(t, 3)
	e := m.accts[0].Lists["INBOX"][0]
	m.onKey(key("i")) // mark read
	m.onKey(key("d")) // then delete the same mail
	// a list fetched before the server saw anything must not bring it back
	stale, _ := mem.List(context.Background(), "INBOX", 100)
	m.onList(listMsg{acct: 0, folder: "INBOX", envs: stale})
	for _, x := range m.accts[0].Lists["INBOX"] {
		if x.UID == e.UID {
			t.Fatal("pending delete reappeared from a stale list")
		}
	}
	staleTrash, _ := mem.List(context.Background(), "Trash", 100)
	m.onList(listMsg{acct: 0, folder: "Trash", envs: staleTrash})
	if len(m.accts[0].Lists["Trash"]) != 1 {
		t.Fatal("local trash copy lost on refresh")
	}
	drainOps(t, m)
	tl, _ := mem.List(context.Background(), "Trash", 10)
	if len(tl) != 1 || !tl[0].Seen {
		t.Fatalf("flag must reach the server before the move: %+v", tl)
	}
}

// End to end against the local mail server: the list changes on the key
// press, before any server round trip; the server follows in background.
func TestDeleteIsInstantWithRealServer(t *testing.T) {
	ts, err := testserver.Start("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	ts.AddUser("ahmet@test.local", "gizli")
	srv := func(addr string) discover.Server {
		h, p, _ := net.SplitHostPort(addr)
		n, _ := strconv.Atoi(p)
		return discover.Server{Host: h, Port: n, Security: discover.None}
	}
	b := backend.NewIMAP(backend.Config{Incoming: srv(ts.IMAPAddr), Outgoing: srv(ts.SMTPAddr), Username: "ahmet@test.local", Password: "gizli"})
	defer b.Close()
	m := opsModel(t, b, func(s string, d time.Time) uint32 {
		raw, _ := mailmsg.Build(&mailmsg.Outgoing{From: &mail.Address{Address: "a@x.com"}, To: []*mail.Address{{Address: "ahmet@test.local"}}, Subject: s, Body: "x", Date: d})
		ts.Deliver("ahmet@test.local", "INBOX", raw)
		return 0
	}, 4)
	start := time.Now()
	for i := 0; i < 2; i++ {
		m.onKey(key("d"))
	}
	if el := time.Since(start); el > 20*time.Millisecond {
		t.Fatalf("key handling took %v", el)
	}
	if len(m.accts[0].Lists["INBOX"]) != 2 {
		t.Fatal("rows not removed at once")
	}
	if serverCount(t, b, "INBOX") != 4 {
		t.Fatal("expected the server to still have them (change runs in background)")
	}
	drainOps(t, m)
	if serverCount(t, b, "INBOX") != 2 || serverCount(t, b, "Trash") != 2 {
		t.Fatal("server did not follow")
	}
}
