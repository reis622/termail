package undo

import "testing"

func TestLIFOAndConfirm(t *testing.T) {
	var s Stack
	a := s.Push(Entry{Subject: "A", From: "INBOX", OrigUID: 1})
	b := s.Push(Entry{Subject: "B", From: "INBOX", OrigUID: 2})
	if _, ok, pending := s.Pop(); ok || !pending {
		t.Fatal("pending entry must not pop")
	}
	s.Confirm(b, 77)
	s.Confirm(a, 76)
	e, ok, _ := s.Pop()
	if !ok || e.Subject != "B" || e.TrashUID != 77 {
		t.Fatalf("got %+v", e)
	}
	e, ok, _ = s.Pop()
	if !ok || e.Subject != "A" || e.TrashUID != 76 {
		t.Fatalf("got %+v", e)
	}
	if _, ok, _ := s.Pop(); ok {
		t.Fatal("expected empty")
	}
}

func TestLimitAndDrop(t *testing.T) {
	var s Stack
	ids := []int{}
	for i := 0; i < 12; i++ {
		ids = append(ids, s.Push(Entry{OrigUID: uint32(i)}))
	}
	if s.Len() != Limit {
		t.Fatalf("len=%d", s.Len())
	}
	if s.Confirm(ids[0], 5) {
		t.Fatal("oldest entry should be evicted")
	}
	s.Drop(ids[11])
	e, _ := s.Peek()
	if e.OrigUID != 10 {
		t.Fatalf("peek=%d", e.OrigUID)
	}
}

func TestBlockUndo(t *testing.T) {
	var s Stack
	s.Push(Entry{Op: OpDelete, Subject: "silinen"})
	id := s.Push(Entry{Op: OpBlock, From: "INBOX", Trash: "Spam", OrigUID: 4, BlockAccount: "a@x.com", BlockKind: "domain", BlockValue: "reklam.com", BlockAdded: true})
	s.Confirm(id, 12)
	e, ok, _ := s.Pop()
	if !ok || e.Op != OpBlock || e.BlockValue != "reklam.com" || e.Trash != "Spam" || e.TrashUID != 12 || !e.BlockAdded {
		t.Fatalf("got %+v", e)
	}
	if e2, _ := s.Peek(); e2.Op != OpDelete {
		t.Fatal("block undo must not touch earlier deletes")
	}
}
