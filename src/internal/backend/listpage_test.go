package backend

import (
	"context"
	"testing"
	"time"
)

func TestDedicatedBodySession(t *testing.T) {
	b := NewIMAP(Config{})
	if b.body == nil || b.body == b.main || b.body == b.ops || b.body == b.srch || b.body == b.list {
		t.Fatal("FetchRaw must use a dedicated IMAP session, not the list/sync one")
	}
	if b.list == nil || b.list == b.main || b.list == b.ops || b.list == b.srch {
		t.Fatal("header pages must use a dedicated list session, not Folders/Flagged on main")
	}
	if b.pref == nil || b.pref == b.list || b.pref == b.main || b.pref == b.body {
		t.Fatal("prefetch must use a second list session so later pages can overlap")
	}
}

func TestMemoryListPageNewestByDateNotUID(t *testing.T) {
	m := NewMemory([]Folder{{Name: "INBOX", Role: RoleInbox}})
	now := time.Now()
	m.Add("INBOX", Envelope{Subject: "NEWEST", Date: now}, []byte("Subject: NEWEST\r\n\r\nx"))
	for i := 1; i <= 5; i++ {
		m.Add("INBOX", Envelope{Subject: "old", Date: now.Add(-time.Duration(i) * time.Hour)}, []byte("Subject: old\r\n\r\nx"))
	}
	page, total, err := m.ListFlagPage(context.Background(), "INBOX", true, 0, 3)
	if err != nil || total != 6 || len(page) != 3 || page[0].Subject != "NEWEST" || page[0].UID != 1 {
		t.Fatalf("first page should be newest by date, not highest UID: n=%d total=%d err=%v subj=%v uid=%v",
			len(page), total, err, subjectsOf(page), uidsOf(page))
	}
}

func subjectsOf(l []Envelope) []string {
	var s []string
	for _, e := range l {
		s = append(s, e.Subject)
	}
	return s
}

func TestMemoryListPageAndFlagPage(t *testing.T) {
	m := NewMemory([]Folder{{Name: "INBOX", Role: RoleInbox}})
	now := time.Now()
	for i := 1; i <= 10; i++ {
		m.Add("INBOX", Envelope{Subject: "m", Date: now.Add(time.Duration(i) * time.Minute), Seen: i <= 4}, []byte("Subject: m\r\n\r\nx"))
	}
	ctx := context.Background()
	page, total, err := m.ListPage(ctx, "INBOX", 0, 3)
	if err != nil || total != 10 || len(page) != 3 || page[0].UID != 10 || page[2].UID != 8 {
		t.Fatalf("newest page: n=%d total=%d err=%v uids=%v", len(page), total, err, uidsOf(page))
	}
	page, total, err = m.ListPage(ctx, "INBOX", 3, 3)
	if err != nil || total != 10 || len(page) != 3 || page[0].UID != 7 {
		t.Fatalf("second page: n=%d total=%d err=%v uids=%v", len(page), total, err, uidsOf(page))
	}
	unseen, ut, err := m.ListFlagPage(ctx, "INBOX", true, 0, 4)
	if err != nil || ut != 6 || len(unseen) != 4 || unseen[0].Seen {
		t.Fatalf("unseen: n=%d total=%d err=%v seen=%v", len(unseen), ut, err, unseen)
	}
	unseen2, ut, err := m.ListFlagPage(ctx, "INBOX", true, 4, 4)
	if err != nil || ut != 6 || len(unseen2) != 2 {
		t.Fatalf("unseen rest: n=%d total=%d err=%v", len(unseen2), ut, err)
	}
	seen, st, err := m.ListFlagPage(ctx, "INBOX", false, 0, 10)
	if err != nil || st != 4 || len(seen) != 4 {
		t.Fatalf("seen: n=%d total=%d err=%v", len(seen), st, err)
	}
}

func uidsOf(l []Envelope) []uint32 {
	var u []uint32
	for _, e := range l {
		u = append(u, e.UID)
	}
	return u
}
