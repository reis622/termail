package backend_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"termail/internal/backend"
	"termail/internal/mailmsg"
	"termail/internal/samplemail"
	"termail/internal/testserver"
)

// Realistic provider mails go through the real protocol stack (list +
// full fetch without marking read) and must come out as clean text.
func TestRealServerSampleMails(t *testing.T) {
	ts, err := testserver.Start("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	ts.AddUser("ahmet@test.local", "gizli")
	samples := samplemail.All(time.Now())
	for _, s := range samples {
		if err := ts.Deliver("ahmet@test.local", "INBOX", s.Raw); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b := backend.NewIMAP(backend.Config{Incoming: srvOf(ts.IMAPAddr), Outgoing: srvOf(ts.SMTPAddr), Username: "ahmet@test.local", Password: "gizli"})
	if err := b.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	l, err := b.List(ctx, "INBOX", 100)
	if err != nil || len(l) != len(samples) {
		t.Fatalf("list %d %v", len(l), err)
	}
	bySubj := map[string]samplemail.Sample{}
	for _, s := range samples {
		bySubj[s.Subject] = s
	}
	for _, e := range l {
		s, ok := bySubj[e.Subject]
		if !ok {
			t.Fatalf("list subject not decoded: %q", e.Subject)
		}
		raw, err := b.FetchRaw(ctx, "INBOX", e.UID)
		if err != nil {
			t.Fatal(err)
		}
		p, err := mailmsg.Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", s.Key, err)
		}
		if !strings.Contains(p.Text, s.WantText) {
			t.Fatalf("%s: body %q", s.Key, p.Text)
		}
		for _, bad := range samplemail.Banned {
			if strings.Contains(p.Text, bad) {
				t.Fatalf("%s: raw header %q in body", s.Key, bad)
			}
		}
		if e.Seen {
			t.Fatalf("%s: fetching marked it read", s.Key)
		}
	}
	l2, _ := b.List(ctx, "INBOX", 100)
	for _, e := range l2 {
		if e.Seen {
			t.Fatalf("%q marked read by preview fetch", e.Subject)
		}
	}
}
