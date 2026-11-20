package backend

import (
	"context"
	"testing"
	"time"
)

func seedInbox(t *testing.T, n int) *Memory {
	t.Helper()
	m := NewMemory([]Folder{{Name: "INBOX", Role: RoleInbox}})
	now := time.Now()
	for i := 1; i <= n; i++ {
		m.Add("INBOX", Envelope{Subject: "m", Date: now.Add(time.Duration(i) * time.Second)}, []byte("Subject: m\r\n\r\nx"))
	}
	return m
}

func TestSyncFolderFlagChangeAndDeleteAndNew(t *testing.T) {
	m := seedInbox(t, 5)
	ctx := context.Background()
	st := m.LastFolderSync("INBOX")
	known := []uint32{1, 2, 3, 4, 5}
	if err := m.SetFlag(ctx, "INBOX", 3, FlagSeen, true); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "INBOX", 2); err != nil {
		t.Fatal(err)
	}
	m.Add("INBOX", Envelope{Subject: "yeni", Date: time.Now()}, []byte("Subject: yeni\r\n\r\nx"))
	d, err := m.SyncFolder(ctx, st, known)
	if err != nil || d.FullReset {
		t.Fatalf("delta: %+v %v", d, err)
	}
	if len(d.Vanished) != 1 || d.Vanished[0] != 2 {
		t.Fatalf("vanished: %v", d.Vanished)
	}
	if len(d.New) != 1 || d.New[0].Subject != "yeni" || d.New[0].UID != 6 {
		t.Fatalf("new: %+v", d.New)
	}
	var saw bool
	for _, e := range d.Flags {
		if e.UID == 3 && e.Seen {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("flag change missing: %+v", d.Flags)
	}
}

func TestSyncFolderUIDValidityChangeResets(t *testing.T) {
	m := seedInbox(t, 3)
	st := m.LastFolderSync("INBOX")
	m.BumpUIDValidity("INBOX")
	d, err := m.SyncFolder(context.Background(), st, []uint32{1, 2, 3})
	if err != nil || !d.FullReset {
		t.Fatalf("want full reset after UIDVALIDITY change, got %+v %v", d, err)
	}
	if d.Sync.UIDValidity == st.UIDValidity {
		t.Fatal("bookmark not advanced")
	}
}

func TestSyncFolderNoCondstoreFallback(t *testing.T) {
	m := seedInbox(t, 4)
	m.DisableCondstore = true
	ctx := context.Background()
	st := m.LastFolderSync("INBOX")
	st.HighestModSeq = 0 // no bookmark as on a server without CONDSTORE
	if err := m.SetFlag(ctx, "INBOX", 1, FlagSeen, true); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "INBOX", 4); err != nil {
		t.Fatal(err)
	}
	m.Add("INBOX", Envelope{Subject: "son", Date: time.Now()}, []byte("Subject: son\r\n\r\nx"))
	d, err := m.SyncFolder(ctx, st, []uint32{1, 2, 3, 4})
	if err != nil || d.FullReset {
		t.Fatalf("%+v %v", d, err)
	}
	if len(d.Vanished) != 1 || d.Vanished[0] != 4 {
		t.Fatalf("vanished: %v", d.Vanished)
	}
	if len(d.New) != 1 || d.New[0].Subject != "son" {
		t.Fatalf("new: %+v", d.New)
	}
	var saw bool
	for _, e := range d.Flags {
		if e.UID == 1 && e.Seen {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("fallback flags: %+v", d.Flags)
	}
}

func TestUidDiff(t *testing.T) {
	added, vanished := uidDiff([]uint32{1, 2, 3}, []uint32{2, 3, 4})
	if len(added) != 1 || added[0] != 4 || len(vanished) != 1 || vanished[0] != 1 {
		t.Fatalf("added=%v vanished=%v", added, vanished)
	}
}
