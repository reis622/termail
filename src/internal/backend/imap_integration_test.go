package backend_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/emersion/go-message/mail"

	"termail/internal/backend"
	"termail/internal/discover"
	"termail/internal/downloads"
	"termail/internal/mailmsg"
	"termail/internal/testserver"
)

func srvOf(addr string) discover.Server {
	h, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return discover.Server{Host: h, Port: n, Security: discover.None}
}

func msg(from, subj, body string) []byte {
	raw, _ := mailmsg.Build(&mailmsg.Outgoing{From: &mail.Address{Name: from, Address: "x@ornek.com"},
		To: []*mail.Address{{Address: "ahmet@test.local"}}, Subject: subj, Body: body})
	return raw
}

func TestRealServerFlow(t *testing.T) {
	ts, err := testserver.Start("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	ts.AddUser("ahmet@test.local", "gizli")
	ts.Deliver("ahmet@test.local", "INBOX", msg("Mehmet Kaya", "Akşam yemeği?", "Bu akşam?"))
	ts.Deliver("ahmet@test.local", "INBOX", msg("Ayşe Demir", "Proje", "Toplantı"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// wrong password
	bad := backend.NewIMAP(backend.Config{Incoming: srvOf(ts.IMAPAddr), Outgoing: srvOf(ts.SMTPAddr), Username: "ahmet@test.local", Password: "yanlis"})
	if err := bad.Connect(ctx); err == nil {
		t.Fatal("wrong password accepted")
	}

	b := backend.NewIMAP(backend.Config{Incoming: srvOf(ts.IMAPAddr), Outgoing: srvOf(ts.SMTPAddr), Username: "ahmet@test.local", Password: "gizli"})
	if err := b.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	fs, err := b.Folders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[backend.Role]string{}
	for _, f := range fs {
		roles[f.Role] = f.Name
	}
	if roles[backend.RoleInbox] != "INBOX" || roles[backend.RoleTrash] != "Trash" || roles[backend.RoleSent] != "Sent" {
		t.Fatalf("roles: %+v", fs)
	}
	l, err := b.List(ctx, "INBOX", 100)
	if err != nil || len(l) != 2 || l[0].Subject != "Proje" {
		t.Fatalf("list %+v %v", l, err)
	}

	// IDLE: a new message must be pushed
	events := make(chan backend.Event, 10)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go b.Watch(wctx, "INBOX", func(e backend.Event) { events <- e })
	waitKind(t, events, backend.EvOnline)
	if err := ts.Deliver("ahmet@test.local", "INBOX", msg("GitHub", "Derleme tamam", "ok")); err != nil {
		t.Fatal(err)
	}
	waitKind(t, events, backend.EvNewMail)

	// delete (move to trash) + undo
	l, _ = b.List(ctx, "INBOX", 100)
	victim := l[0]
	trashUID, err := b.Move(ctx, "INBOX", victim.UID, "Trash", victim.MessageID)
	if err != nil || trashUID == 0 {
		t.Fatalf("move: %d %v", trashUID, err)
	}
	if l2, _ := b.List(ctx, "INBOX", 100); len(l2) != 2 {
		t.Fatalf("after delete %d", len(l2))
	}
	back, err := b.Move(ctx, "Trash", trashUID, "INBOX", victim.MessageID)
	if err != nil || back == 0 {
		t.Fatalf("undo: %v", err)
	}
	if l3, _ := b.List(ctx, "INBOX", 100); len(l3) != 3 {
		t.Fatalf("after undo %d", len(l3))
	}
	// permanent delete from trash
	tu, _ := b.Move(ctx, "INBOX", back, "Trash", "")
	if err := b.Delete(ctx, "Trash", tu); err != nil {
		t.Fatal(err)
	}
	if lt, _ := b.List(ctx, "Trash", 100); len(lt) != 0 {
		t.Fatal("trash not empty")
	}

	// flags
	if err := b.SetFlag(ctx, "INBOX", l[1].UID, backend.FlagSeen, true); err != nil {
		t.Fatal(err)
	}
	// save for later = \Flagged; SEARCH FLAGGED finds it, removing clears it
	if err := b.SetFlag(ctx, "INBOX", l[1].UID, backend.FlagFlagged, true); err != nil {
		t.Fatal(err)
	}
	if fl, err := b.Flagged(ctx, "INBOX", 50); err != nil || len(fl) != 1 || fl[0].UID != l[1].UID || !fl[0].Flagged {
		t.Fatalf("flagged search: %+v %v", fl, err)
	}
	if err := b.SetFlag(ctx, "INBOX", l[1].UID, backend.FlagFlagged, false); err != nil {
		t.Fatal(err)
	}
	if fl, _ := b.Flagged(ctx, "INBOX", 50); len(fl) != 0 {
		t.Fatalf("flag not removed: %+v", fl)
	}
	// a STORE on a UID that is gone is reported, not silently "done"
	if err := b.SetFlag(ctx, "INBOX", 999999, backend.FlagFlagged, true); !errors.Is(err, backend.ErrMsgGone) {
		t.Fatalf("store on a missing UID: %v", err)
	}
	if err := b.SetFlagMany(ctx, "INBOX", []uint32{l[1].UID, 999998}, backend.FlagSeen, true); !errors.Is(err, backend.ErrMsgGone) {
		t.Fatalf("batch store with a missing UID: %v", err)
	}
	if err := b.SetFlagMany(ctx, "INBOX", []uint32{l[1].UID}, backend.FlagSeen, false); err != nil {
		t.Fatalf("batch unseen: %v", err)
	}

	// send with attachment to self -> SMTP sink + Sent copy + arrives in INBOX
	raw, _ := mailmsg.Build(&mailmsg.Outgoing{From: &mail.Address{Name: "Ahmet", Address: "ahmet@test.local"},
		To: []*mail.Address{{Address: "ahmet@test.local"}}, Subject: "Ekli ileti", Body: "Ekte.",
		Attachments: []mailmsg.Attachment{{Name: "rapor.pdf", Data: []byte("%PDF-1.4 test")}}})
	if err := b.Send(ctx, "ahmet@test.local", []string{"ahmet@test.local"}, raw, "Sent"); err != nil {
		t.Fatal(err)
	}
	if r, ok := ts.LastReceived(); !ok || len(r.To) != 1 {
		t.Fatal("smtp sink got nothing")
	}
	if ls, _ := b.List(ctx, "Sent", 10); len(ls) != 1 || !ls[0].Seen || !ls[0].Attach {
		t.Fatalf("sent copy: %+v", ls)
	}
	waitKind(t, events, backend.EvNewMail)
	li, _ := b.List(ctx, "INBOX", 10)
	if li[0].Subject != "Ekli ileti" || !li[0].Attach {
		t.Fatalf("inbox top %+v", li[0])
	}
	body, err := b.FetchRaw(ctx, "INBOX", li[0].UID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := mailmsg.Parse(body)
	if err != nil || len(p.Attachments) != 1 {
		t.Fatalf("parse %v", err)
	}
	dir := t.TempDir()
	p1, _ := downloads.Save(dir, p.Attachments[0].Name, p.Attachments[0].Data)
	p2, _ := downloads.Save(dir, p.Attachments[0].Name, p.Attachments[0].Data)
	if p1 == p2 || len(p2) == 0 {
		t.Fatal("save attachment")
	}
	// fetching body must not mark it read
	if li2, _ := b.List(ctx, "INBOX", 10); li2[0].Seen {
		t.Fatal("peek marked as read")
	}
	if res, err := b.Search(ctx, "INBOX", "Akşam", 10); err != nil || len(res) != 1 {
		t.Fatalf("search %v %v", res, err)
	}
	// structured search on its own connection: words, sender, attachment
	if res, err := b.SearchCriteria(ctx, "INBOX", backend.Criteria{Text: []string{"Akşam"}}, 10); err != nil || len(res) != 1 {
		t.Fatalf("criteria search %v %v", res, err)
	}
	if res, err := b.SearchCriteria(ctx, "INBOX", backend.Criteria{Attach: true}, 10); err != nil || len(res) != 1 || res[0].Subject != "Ekli ileti" {
		t.Fatalf("attach search %v %v", res, err)
	}
	if res, err := b.SearchCriteria(ctx, "INBOX", backend.Criteria{Text: []string{"yokboylebirkelime"}}, 10); err != nil || len(res) != 0 {
		t.Fatalf("empty search %v %v", res, err)
	}
	cctx, ccancel := context.WithCancel(ctx)
	ccancel() // a search that is no longer wanted returns at once
	if _, err := b.SearchCriteria(cctx, "INBOX", backend.Criteria{Text: []string{"Akşam"}}, 10); err == nil {
		t.Fatal("cancelled search ran")
	}
	if res, err := b.SearchCriteria(ctx, "INBOX", backend.Criteria{Text: []string{"Akşam"}}, 10); err != nil || len(res) != 1 {
		t.Fatalf("search after cancel %v %v", res, err)
	}
	if err := b.Append(ctx, "Drafts", raw, []backend.Flag{backend.FlagDraft}); err != nil {
		t.Fatal(err)
	}
}

func waitKind(t *testing.T, ch chan backend.Event, k backend.EventKind) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Kind == k {
				return
			}
		case <-timeout:
			t.Fatalf("timeout waiting for event %d", k)
		}
	}
}

func msgAt(from, subj, body string, d time.Time) []byte {
	raw, _ := mailmsg.Build(&mailmsg.Outgoing{From: &mail.Address{Name: from, Address: "x@ornek.com"},
		To: []*mail.Address{{Address: "ahmet@test.local"}}, Subject: subj, Body: body, Date: d})
	return raw
}

func TestListFlagPageFirstIsNewestByDate(t *testing.T) {
	ts, err := testserver.Start("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	ts.AddUser("ahmet@test.local", "gizli")
	now := time.Now()
	// Low UID, newest date — then older mail with higher UIDs (the old
	// highest-UID window would hide this on the first page).
	if err := ts.DeliverAt("ahmet@test.local", "INBOX", msgAt("Mehmet", "NEWEST", "hi", now), now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		at := now.Add(-time.Duration(i+1) * time.Hour)
		if err := ts.DeliverAt("ahmet@test.local", "INBOX", msgAt("Toplu", fmt.Sprintf("OLD-%02d", i), "x", at), at); err != nil {
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
	page, total, err := b.ListFlagPage(ctx, "INBOX", true, 0, 5)
	if err != nil || total != 21 || len(page) == 0 || page[0].Subject != "NEWEST" {
		t.Fatalf("first page should be newest by date, not highest UID: total=%d err=%v page=%v", total, err, subjects(page))
	}
}

func subjects(l []backend.Envelope) []string {
	var s []string
	for _, e := range l {
		s = append(s, e.Subject)
	}
	return s
}


func TestSyncFolderNoCondstoreOnTestServer(t *testing.T) {
	ts, err := testserver.Start("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	ts.AddUser("ahmet@test.local", "gizli")
	now := time.Now()
	for i := 0; i < 4; i++ {
		if err := ts.DeliverAt("ahmet@test.local", "INBOX", msgAt("X", fmt.Sprintf("M-%d", i), "x", now.Add(time.Duration(i)*time.Second)), now.Add(time.Duration(i)*time.Second)); err != nil {
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
	list, err := b.List(ctx, "INBOX", 0)
	if err != nil || len(list) != 4 {
		t.Fatalf("list %d %v", len(list), err)
	}
	st := b.LastFolderSync("INBOX")
	if st.UIDValidity == 0 && st.UIDNext == 0 {
		t.Fatal("expected a SELECT bookmark after List")
	}
	known := make([]uint32, len(list))
	for i, e := range list {
		known[i] = e.UID
	}
	// flag, delete, append while "closed" (no further list pages)
	if err := b.SetFlag(ctx, "INBOX", known[0], backend.FlagSeen, true); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, "INBOX", known[1]); err != nil {
		t.Fatal(err)
	}
	if err := ts.DeliverAt("ahmet@test.local", "INBOX", msgAt("X", "YENI", "x", time.Now()), time.Now()); err != nil {
		t.Fatal(err)
	}
	d, err := b.SyncFolder(ctx, st, known)
	if err != nil || d.FullReset {
		t.Fatalf("sync %+v %v", d, err)
	}
	if len(d.Vanished) != 1 || d.Vanished[0] != known[1] {
		t.Fatalf("vanished %v want %d", d.Vanished, known[1])
	}
	if len(d.New) != 1 || d.New[0].Subject != "YENI" {
		t.Fatalf("new %+v", d.New)
	}
	var saw bool
	for _, e := range d.Flags {
		if e.UID == known[0] && e.Seen {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("flag change missing: %+v", d.Flags)
	}
}


func TestInboxColdVsWarmTiming(t *testing.T) {
	const n = 3800
	ts, err := testserver.Start("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()
	ts.AddUser("ahmet@test.local", "gizli")
	now := time.Now()
	for i := 0; i < n; i++ {
		d := now.Add(time.Duration(i) * time.Second)
		if err := ts.DeliverAt("ahmet@test.local", "INBOX", msgAt("X", fmt.Sprintf("M-%d", i), "x", d), d); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	b := backend.NewIMAP(backend.Config{Incoming: srvOf(ts.IMAPAddr), Outgoing: srvOf(ts.SMTPAddr), Username: "ahmet@test.local", Password: "gizli"})
	if err := b.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	t0 := time.Now()
	var all []backend.Envelope
	off, total := 0, n
	for off < total {
		page, tot, err := b.ListFlagPage(ctx, "INBOX", true, off, 250)
		if err != nil {
			t.Fatal(err)
		}
		if tot > 0 {
			total = tot
		}
		all = append(all, page...)
		off += len(page)
		if len(page) == 0 {
			break
		}
	}
	cold := time.Since(t0)
	if len(all) != n {
		t.Fatalf("cold listed %d, want %d", len(all), n)
	}
	st := b.LastFolderSync("INBOX")
	known := make([]uint32, len(all))
	for i, e := range all {
		known[i] = e.UID
	}

	t1 := time.Now()
	d, err := b.SyncFolder(ctx, st, known)
	if err != nil || d.FullReset || len(d.New) != 0 || len(d.Vanished) != 0 {
		t.Fatalf("unchanged sync %+v %v", d, err)
	}
	warmSame := time.Since(t1)

	if err := ts.DeliverAt("ahmet@test.local", "INBOX", msgAt("X", "SON", "x", time.Now()), time.Now()); err != nil {
		t.Fatal(err)
	}
	t2 := time.Now()
	d, err = b.SyncFolder(ctx, d.Sync, known)
	warmNew := time.Since(t2)
	if err != nil || len(d.New) != 1 || d.New[0].Subject != "SON" {
		t.Fatalf("new-mail sync %+v %v", d, err)
	}
	t.Logf("3800-mail testserver timings: cold paged load %s, warm unchanged SyncFolder %s, warm +1 SyncFolder %s", cold, warmSame, warmNew)
	if warmSame >= cold {
		t.Logf("note: unchanged sync was not faster than a cold paged load (cold=%s warm=%s)", cold, warmSame)
	}
}
