package toast

import (
	"testing"
	"time"
)

func TestActionToastsExpire(t *testing.T) {
	var m Manager
	now := time.Unix(1000, 0)
	m.Push(Deleted, "Silindi:", "Kitap önerisi", now, 0)
	m.Push(Success, "Kaydedildi:", "C:\\Users\\a\\Downloads\\f.pdf", now.Add(time.Second), 0)
	if len(m.Actions()) != 2 {
		t.Fatal("want 2")
	}
	m.Tick(now.Add(4*time.Second + time.Millisecond))
	if len(m.Actions()) != 1 || m.Actions()[0].Label != "Kaydedildi:" {
		t.Fatalf("got %+v", m.Actions())
	}
	m.Tick(now.Add(10 * time.Second))
	if len(m.Actions()) != 0 {
		t.Fatal("should be empty")
	}
}

func TestNewMailIsSticky(t *testing.T) {
	var m Manager
	now := time.Unix(1000, 0)
	m.AddMail(Mail{Account: 1, Folder: "INBOX", UID: 5, From: "Ayşe", At: now})
	m.Tick(now.Add(time.Hour))
	if !m.HasMail() || m.Title() != "Yeni ileti" {
		t.Fatal("new mail toast must not expire")
	}
}

func TestMergeTargetAndRead(t *testing.T) {
	var m Manager
	m.AddMail(Mail{Account: 0, Folder: "INBOX", UID: 1, From: "A"})
	m.AddMail(Mail{Account: 1, Folder: "INBOX", UID: 9, From: "B"})
	m.AddMail(Mail{Account: 0, Folder: "INBOX", UID: 2, From: "C"})
	m.AddMail(Mail{Account: 0, Folder: "INBOX", UID: 2, From: "C"}) // duplicate
	if m.Title() != "3 yeni ileti" {
		t.Fatalf("title %q", m.Title())
	}
	if tg, _ := m.Target(); tg.From != "C" {
		t.Fatalf("target should be newest, got %q", tg.From)
	}
	if got := m.Mails(); got[0].From != "C" || got[2].From != "A" {
		t.Fatalf("order %+v", got)
	}
	if !m.MarkRead(0, "INBOX", 2) {
		t.Fatal("markread")
	}
	if m.MarkRead(0, "INBOX", 99) {
		t.Fatal("unknown uid")
	}
	if tg, _ := m.Target(); tg.From != "B" || m.Title() != "2 yeni ileti" {
		t.Fatalf("after read: %+v %s", tg, m.Title())
	}
	m.DismissMail()
	if m.HasMail() {
		t.Fatal("dismiss")
	}
	if _, ok := m.Target(); ok {
		t.Fatal("no target after dismiss")
	}
}

func TestActionCap(t *testing.T) {
	var m Manager
	now := time.Now()
	for i := 0; i < 5; i++ {
		m.Push(Error, "", "err"+string(rune('a'+i)), now, 0)
	}
	if len(m.Actions()) != 3 {
		t.Fatalf("cap: %d", len(m.Actions()))
	}
	// action toasts never affect sticky mail toasts
	m.AddMail(Mail{UID: 1})
	m.Tick(now.Add(time.Minute))
	if !m.HasMail() || len(m.Actions()) != 0 {
		t.Fatal("tick mixing kinds")
	}
}

func TestSameKindToastsMerge(t *testing.T) {
	var m Manager
	now := time.Unix(1000, 0)
	id1 := m.Push(Deleted, "Silindi:", "Bir", now, 0)
	id2 := m.Push(Deleted, "Silindi:", "İki", now, 0)
	id3 := m.Push(Deleted, "Silindi:", "Üç", now, 0)
	if id1 != id2 || id2 != id3 {
		t.Fatalf("deletes should reuse one toast id, got %d %d %d", id1, id2, id3)
	}
	if len(m.Actions()) != 1 {
		t.Fatalf("want 1 merged toast, got %d", len(m.Actions()))
	}
	a := m.Actions()[0]
	if a.Count != 3 || a.Text != "3 ileti silindi" {
		t.Fatalf("merged delete: %+v", a)
	}
	m.Push(Success, "Kaydedildi:", "a.pdf", now, 0)
	m.Push(Success, "Kaydedildi:", "b.pdf", now, 0)
	if len(m.Actions()) != 2 {
		t.Fatalf("success should not merge with delete: %d", len(m.Actions()))
	}
	var errM Manager
	errM.MaxActions = 10
	errM.Push(Error, "", "ağ hatası", now, 0)
	errM.Push(Error, "", "ağ hatası", now, 0)
	errM.Push(Error, "", "sunucu yok", now, 0)
	if len(errM.Actions()) != 2 {
		t.Fatalf("errors should stay separate but deduped, got %d %+v", len(errM.Actions()), errM.Actions())
	}
	m.SetCount(id1, 2, now)
	if m.Actions()[0].Text != "2 ileti silindi" || m.Actions()[0].Count != 2 {
		t.Fatalf("setcount: %+v", m.Actions()[0])
	}
	m.SetCount(id1, 0, now)
	if len(m.Actions()) != 1 || m.Actions()[0].Kind != Success {
		t.Fatalf("setcount 0 should drop the toast, got %+v", m.Actions())
	}
}
