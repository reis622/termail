package backend_test

import (
	"context"
	"testing"
	"time"

	"termail/internal/backend"
	"termail/internal/discover"
	"termail/internal/testserver"
)

// The outgoing connection is opened ahead (Prewarm) and reused; a generic
// server gets a copy in Sent, Gmail / Outlook do not (they save it).
func TestSendReusesConnectionAndSavesCopy(t *testing.T) {
	ts, err := testserver.Start("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	ts.AddUser("ahmet@test.local", "gizli")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b := backend.NewIMAP(backend.Config{Incoming: srvOf(ts.IMAPAddr), Outgoing: srvOf(ts.SMTPAddr), Username: "ahmet@test.local", Password: "gizli"})
	defer b.Close()
	if err := b.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Prewarm(ctx); err != nil {
		t.Fatal(err)
	}
	if b.ServerSavesSent() {
		t.Fatal("local server does not save sent mail itself")
	}
	for i := 0; i < 3; i++ {
		if err := b.Send(ctx, "ahmet@test.local", []string{"ahmet@test.local"}, msg("Ahmet", "deneme", "gövde"), "Sent"); err != nil {
			t.Fatal(err)
		}
	}
	if n := ts.Conns(); n != 1 {
		t.Fatalf("%d outgoing connections for prewarm + 3 sends, want 1", n)
	}
	if n, _ := b.Count(ctx, "Sent"); n != 3 {
		t.Fatalf("Sent has %d copies, want 3", n)
	}
	// a stale connection (server closed it) is replaced transparently
	ts.Close()
	if err := b.Send(ctx, "ahmet@test.local", []string{"x@y.z"}, msg("A", "b", "c"), ""); err == nil {
		t.Fatal("send to a stopped server must fail")
	}
}

func TestServerSavesSent(t *testing.T) {
	for host, want := range map[string]bool{"imap.gmail.com": true, "outlook.office365.com": true, "imap.yandex.com": false, "mail.firma.com.tr": false} {
		b := backend.NewIMAP(backend.Config{Incoming: discover.Server{Host: host, Port: 993, Security: discover.TLS}})
		if got := b.ServerSavesSent(); got != want {
			t.Errorf("%s: %v", host, got)
		}
	}
}
