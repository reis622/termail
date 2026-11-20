package opqueue

import (
	"testing"
	"time"

	"termail/internal/backend"
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func mv(uid uint32, dest string) Op {
	return Op{Kind: Move, Acct: 0, Folder: "INBOX", UID: uid, Dest: dest}
}

func TestCoalesceWithinWindow(t *testing.T) {
	q := New(300 * time.Millisecond)
	for i := 0; i < 5; i++ {
		q.Add(mv(uint32(10+i), "Trash"), t0.Add(time.Duration(i)*50*time.Millisecond))
	}
	if b := q.Ready(t0.Add(200 * time.Millisecond)); len(b) != 0 {
		t.Fatal("started before the window passed")
	}
	if d, ok := q.Next(t0.Add(200 * time.Millisecond)); !ok || d != 100*time.Millisecond {
		t.Fatalf("next %v %v", d, ok)
	}
	bs := q.Ready(t0.Add(300 * time.Millisecond))
	if len(bs) != 1 || len(bs[0].Ops) != 5 || bs[0].Dest != "Trash" {
		t.Fatalf("%+v", bs)
	}
	if got := bs[0].UIDs(); got[0] != 10 || got[4] != 14 {
		t.Fatalf("order %v", got)
	}
}

func TestOrderPerMailbox(t *testing.T) {
	q := New(0)
	q.Add(mv(1, "Trash"), t0)
	q.Add(Op{Kind: Flag, Folder: "INBOX", UID: 2, Flag: backend.FlagSeen, On: true}, t0)
	q.Add(mv(3, "Trash"), t0)
	q.Add(Op{Kind: Move, Acct: 0, Folder: "Archive", UID: 9, Dest: "Trash"}, t0)
	bs := q.Ready(t0)
	if len(bs) != 2 {
		t.Fatalf("want one batch per mailbox, got %d", len(bs))
	}
	var inbox *Batch
	for _, b := range bs {
		if b.Folder == "INBOX" {
			inbox = b
		}
	}
	if len(inbox.Ops) != 1 || inbox.Ops[0].UID != 1 {
		t.Fatalf("first run must stop at the incompatible op: %+v", inbox.Ops)
	}
	if more := q.Ready(t0); len(more) != 0 {
		t.Fatal("next INBOX batch started while one is running")
	}
	q.Done(inbox)
	bs = q.Ready(t0)
	if len(bs) != 1 || bs[0].Kind != Flag || bs[0].Ops[0].UID != 2 {
		t.Fatalf("%+v", bs)
	}
	q.Done(bs[0])
	bs = q.Ready(t0)
	if len(bs) != 1 || bs[0].Ops[0].UID != 3 {
		t.Fatalf("%+v", bs)
	}
}

func TestCancelOnlyWhileQueued(t *testing.T) {
	q := New(300 * time.Millisecond)
	a := q.Add(mv(1, "Trash"), t0)
	b := q.Add(mv(2, "Trash"), t0)
	if _, ok := q.Cancel(a); !ok {
		t.Fatal("queued op not cancellable")
	}
	if q.Hidden(0, "INBOX", 1) {
		t.Fatal("cancelled op still hides the message")
	}
	bs := q.Ready(t0.Add(time.Second))
	if len(bs) != 1 || len(bs[0].Ops) != 1 || bs[0].Ops[0].UID != 2 {
		t.Fatalf("%+v", bs)
	}
	if _, ok := q.Cancel(b); ok {
		t.Fatal("running op cancelled")
	}
	if !q.Hidden(0, "INBOX", 2) || len(q.MovesInto(0, "Trash")) != 1 {
		t.Fatal("running move not visible to the overlay")
	}
	q.Done(bs[0])
	if q.Hidden(0, "INBOX", 2) || q.Pending() != 0 {
		t.Fatal("finished op still pending")
	}
}

func TestFlagStateLastWins(t *testing.T) {
	q := New(time.Second)
	q.Add(Op{Kind: Flag, Folder: "INBOX", UID: 5, Flag: backend.FlagSeen, On: true}, t0)
	q.Add(Op{Kind: Flag, Folder: "INBOX", UID: 5, Flag: backend.FlagSeen, On: false}, t0)
	if on, ok := q.FlagState(0, "INBOX", 5, backend.FlagSeen); !ok || on {
		t.Fatalf("%v %v", on, ok)
	}
	if _, ok := q.FlagState(0, "INBOX", 5, backend.FlagFlagged); ok {
		t.Fatal("other flag reported")
	}
	// on then off must not be merged into one batch
	bs := q.Ready(t0.Add(time.Second))
	if len(bs[0].Ops) != 1 || !bs[0].On {
		t.Fatalf("%+v", bs[0])
	}
}

func TestMaxBatchChunksMoves(t *testing.T) {
	q := New(0)
	for i := 1; i <= 450; i++ {
		q.Add(mv(uint32(i), "Trash"), t0)
	}
	var sizes []int
	for {
		bs := q.Ready(t0)
		if len(bs) == 0 {
			break
		}
		if len(bs) != 1 {
			t.Fatalf("one mailbox should yield one batch at a time, got %d", len(bs))
		}
		if n := len(bs[0].Ops); n > 200 {
			t.Fatalf("batch of %d exceeds 200", n)
		}
		sizes = append(sizes, len(bs[0].Ops))
		q.Done(bs[0])
	}
	if len(sizes) != 3 || sizes[0] != 200 || sizes[1] != 200 || sizes[2] != 50 {
		t.Fatalf("want 200+200+50, got %v", sizes)
	}
}
