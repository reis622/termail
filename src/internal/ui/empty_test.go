package ui

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/emersion/go-message/mail"

	"termail/internal/backend"
	"termail/internal/discover"
	"termail/internal/mailmsg"
	"termail/internal/testserver"
	"termail/internal/toast"
)

func TestEmptyableFolders(t *testing.T) {
	yes := []backend.Folder{{Name: "[Gmail]/Trash", Role: backend.RoleTrash}, {Name: "Junk", Role: backend.RoleJunk},
		{Name: "Çöp Kutusu"}, {Name: "Deleted Items"}, {Name: "INBOX.Spam"}, {Name: "[Gmail]/Spam"}, {Name: "Gereksiz E-posta"}}
	no := []backend.Folder{{Name: "INBOX", Role: backend.RoleInbox}, {Name: "Sent", Role: backend.RoleSent}, {Name: "Faturalar"}, {Name: "[Gmail]/All Mail", Role: backend.RoleAll}}
	for _, f := range yes {
		if !emptyable(&f) {
			t.Errorf("%q should be emptyable", f.Name)
		}
	}
	for _, f := range no {
		if emptyable(&f) {
			t.Errorf("%q must not be emptyable", f.Name)
		}
	}
	if emptyable(nil) {
		t.Error("nil folder")
	}
}

func TestEmptyQuestionTurkish(t *testing.T) {
	cases := map[string]string{
		emptyQuestion("Çöp Kutusu", 23): "Çöp Kutusu'ndaki 23 iletinin hepsi kalıcı silinsin mi? e/h",
		emptyQuestion("Spam", 5):        "Spam'deki 5 iletinin hepsi kalıcı silinsin mi? e/h",
		emptyQuestion("Çöp Kutusu", 1):  "Çöp Kutusu'ndaki 1 ileti kalıcı silinsin mi? e/h",
		locativeKi("Gereksiz"):          "Gereksiz'deki",
		locativeKi("Deleted Items"):     "Deleted Items'teki",
		locativeKi("Junk"):              "Junk'taki",
		locativeKi("Silinmiş Öğeler"):   "Silinmiş Öğeler'deki",
		locativeKi("Istenmeyen"):        "Istenmeyen'deki",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestEmptyConfirmOnlyWithE(t *testing.T) {
	for _, k := range []string{"enter", "h", "esc", "y", "space", "down", "x"} {
		m, mem := memModel(t, 0)
		fillTrash(t, m, mem, 3)
		openEmptyDialog(t, m)
		if cmd := m.onKey(key(k)); cmd != nil || m.confirm != nil {
			t.Fatalf("%q confirmed or kept the dialog", k)
		}
		if serverCount(t, mem, "Trash") != 3 {
			t.Fatalf("%q emptied the trash", k)
		}
	}
	m, mem := memModel(t, 0)
	fillTrash(t, m, mem, 3)
	openEmptyDialog(t, m)
	cmd := m.onKey(key("e"))
	if cmd == nil || m.emptying == nil {
		t.Fatal("e did not start emptying")
	}
	m.Update(cmd())
	if serverCount(t, mem, "Trash") != 0 || len(m.accts[0].Lists["Trash"]) != 0 {
		t.Fatal("trash not emptied")
	}
}

func fillTrash(t *testing.T, m *Model, mem *backend.Memory, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		mem.Add("Trash", backend.Envelope{From: "X", Subject: "çöp " + strconv.Itoa(i), Date: time.Now()}, []byte("Subject: x\r\n\r\nx"))
	}
	l, _ := mem.List(context.Background(), "Trash", 100)
	m.accts[0].Lists["Trash"], m.accts[0].Loaded["Trash"] = l, true
	m.folder = "Trash"
	m.cursor = 0
}

func openEmptyDialog(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.onKey(key("D"))
	if cmd == nil {
		t.Fatal("Shift+D did nothing in the trash")
	}
	m.Update(cmd())
	if m.confirm == nil || !m.confirm.strict {
		t.Fatal("no confirmation")
	}
	if !strings.Contains(m.confirm.text, "Çöp Kutusu'ndaki") {
		t.Fatalf("%q", m.confirm.text)
	}
}

func TestShiftDElsewhereOnlyHints(t *testing.T) {
	m, mem := memModel(t, 3)
	// the inbox asks for a bulk move to the trash; Enter alone cancels
	if cmd := m.onKey(key("D")); cmd != nil || m.confirm == nil {
		t.Fatal("Shift+D in the inbox should ask")
	}
	m.onKey(key("enter"))
	if m.confirm != nil || serverCount(t, mem, "INBOX") != 3 {
		t.Fatal("Enter must not confirm the bulk delete")
	}
	m.showUnified() // Tüm Gelen Kutuları asks too
	if m.onKey(key("D")); m.confirm == nil {
		t.Fatal("Shift+D in Tüm Gelen Kutuları should ask")
	}
	m.onKey(key("h"))
	m.showSaved()
	m.toasts = toast.Manager{}
	if cmd := m.onKey(key("D")); cmd != nil || m.confirm != nil || len(m.toasts.Actions()) != 0 {
		t.Fatal("Shift+D must do nothing in Kaydedilenler")
	}
	if serverCount(t, mem, "INBOX") != 3 {
		t.Fatal("inbox touched")
	}
}

func TestEmptyDialogFitsAndKeybarShowsHint(t *testing.T) {
	m, mem := memModel(t, 0)
	fillTrash(t, m, mem, 2)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Kalıcı sil") {
		t.Fatal("trash key bar should label d as Kalıcı sil")
	}
	openEmptyDialog(t, m)
	for _, sz := range sizes {
		m.applySize(sz[0], sz[1])
		checkFrame(t, "empty-dialog", m)
	}
}

// End to end against the local mail server: fill the trash, empty it in
// batches with progress, other folders stay untouched.
func TestEmptyTrashRealServer(t *testing.T) {
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
	raw := func(s string) []byte {
		r, _ := mailmsg.Build(&mailmsg.Outgoing{From: &mail.Address{Address: "a@x.com"}, To: []*mail.Address{{Address: "ahmet@test.local"}}, Subject: s, Body: "x"})
		return r
	}
	for i := 0; i < backend.EmptyBatch+40; i++ {
		ts.Deliver("ahmet@test.local", "Trash", raw("çöp "+strconv.Itoa(i)))
	}
	m := opsModel(t, b, func(s string, d time.Time) uint32 {
		ts.Deliver("ahmet@test.local", "INBOX", raw(s))
		return 0
	}, 3)
	m.folder = "Trash"
	cmd := m.onKey(key("D"))
	m.Update(cmd())
	if m.confirm == nil || !strings.Contains(m.confirm.text, strconv.Itoa(backend.EmptyBatch+40)+" iletinin") {
		t.Fatalf("dialog must show the server count: %+v", m.confirm)
	}
	cmd = m.onKey(key("e"))
	done := cmd()
	progress := 0
	for {
		select {
		case ev := <-m.events:
			if p, ok := ev.(emptyProgressMsg); ok {
				progress++
				m.Update(p)
			}
			continue
		default:
		}
		break
	}
	m.Update(done)
	if progress < 2 {
		t.Fatalf("expected batch progress, got %d updates", progress)
	}
	if serverCount(t, b, "Trash") != 0 || serverCount(t, b, "INBOX") != 3 {
		t.Fatal("trash not empty or inbox touched")
	}
	ok := false
	for _, a := range m.toasts.Actions() {
		ok = ok || a.Label == "Boşaltıldı:"
	}
	if !ok {
		t.Fatal("no Boşaltıldı toast")
	}
}
