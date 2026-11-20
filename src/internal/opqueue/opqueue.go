// Package opqueue orders and batches the background server changes of the
// UI (moves to trash/spam/archive, read and saved flags).
//
// The UI applies every change to its own lists at once (optimistic) and
// adds an Op here. Ops of the same mailbox are kept strictly in order and
// run one batch at a time; consecutive compatible ops arriving within the
// coalescing window (e.g. several quick deletes) become one batch, i.e. one
// UID MOVE / UID STORE with a UID set. While an op is queued or running the
// queue tells the UI how to overlay fresh server lists (hide moved
// messages, keep the new flag) so nothing flickers back. A queued op can
// still be cancelled (undo before the server saw it).
//
// Queue is not safe for concurrent use; it lives in the UI model.
package opqueue

import (
	"time"

	"termail/internal/backend"
)

// Kind of operation.
type Kind int

const (
	Move Kind = iota
	Flag
)

// Op is one change of one message.
type Op struct {
	ID     int
	Kind   Kind
	Acct   int
	Folder string // source mailbox
	UID    uint32
	Dest   string       // Move
	Flag   backend.Flag // Flag
	On     bool         // Flag
	Data   any          // UI payload (envelope, undo id, …)
	added  time.Time
}

// Batch is a run of compatible ops of one mailbox executed together.
type Batch struct {
	ID     int
	Kind   Kind
	Acct   int
	Folder string
	Dest   string
	Flag   backend.Flag
	On     bool
	Ops    []Op
}

// UIDs of the batch, in order.
func (b *Batch) UIDs() []uint32 {
	out := make([]uint32, len(b.Ops))
	for i, o := range b.Ops {
		out[i] = o.UID
	}
	return out
}

type key struct {
	acct   int
	folder string
}

// Queue holds pending ops.
type Queue struct {
	Window   time.Duration // coalescing window (default 300 ms)
	MaxBatch int           // largest batch (default 200)
	queued   map[key][]Op
	running  map[key]*Batch
	nextID   int
}

// New creates a queue with the given coalescing window.
func New(window time.Duration) *Queue {
	return &Queue{Window: window, MaxBatch: 200, queued: map[key][]Op{}, running: map[key]*Batch{}}
}

// Add queues op and returns its id.
func (q *Queue) Add(op Op, now time.Time) int {
	q.nextID++
	op.ID = q.nextID
	op.added = now
	k := key{op.Acct, op.Folder}
	q.queued[k] = append(q.queued[k], op)
	return op.ID
}

func compatible(a, b Op) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == Move {
		return a.Dest == b.Dest
	}
	return a.Flag == b.Flag && a.On == b.On
}

// Ready returns the batches that should start now: for every mailbox with
// nothing running whose oldest queued op has waited the coalescing window,
// the leading run of compatible ops. Started batches count as running until
// Done.
func (q *Queue) Ready(now time.Time) []*Batch {
	var out []*Batch
	for k, ops := range q.queued {
		if len(ops) == 0 || q.running[k] != nil || now.Sub(ops[0].added) < q.Window {
			continue
		}
		n := 1
		for n < len(ops) && n < q.MaxBatch && compatible(ops[0], ops[n]) {
			n++
		}
		q.nextID++
		b := &Batch{ID: q.nextID, Kind: ops[0].Kind, Acct: k.acct, Folder: k.folder, Dest: ops[0].Dest, Flag: ops[0].Flag, On: ops[0].On,
			Ops: append([]Op(nil), ops[:n]...)}
		q.queued[k] = append([]Op(nil), ops[n:]...)
		if len(q.queued[k]) == 0 {
			delete(q.queued, k)
		}
		q.running[k] = b
		out = append(out, b)
	}
	return out
}

// Done marks a batch finished (successfully or not); the next batch of the
// same mailbox may start.
func (q *Queue) Done(b *Batch) {
	k := key{b.Acct, b.Folder}
	if q.running[k] != nil && q.running[k].ID == b.ID {
		delete(q.running, k)
	}
}

// Next reports how long until Ready may return something (ok=false when
// nothing is queued).
func (q *Queue) Next(now time.Time) (time.Duration, bool) {
	best, ok := time.Duration(0), false
	for k, ops := range q.queued {
		if len(ops) == 0 || q.running[k] != nil {
			continue
		}
		d := max(0, q.Window-now.Sub(ops[0].added))
		if !ok || d < best {
			best, ok = d, true
		}
	}
	return best, ok
}

// Cancel removes a still-queued op. It returns false when the op is already
// running or finished (then the caller has to undo it on the server).
func (q *Queue) Cancel(id int) (Op, bool) {
	for k, ops := range q.queued {
		for i, o := range ops {
			if o.ID == id {
				q.queued[k] = append(append([]Op(nil), ops[:i]...), ops[i+1:]...)
				if len(q.queued[k]) == 0 {
					delete(q.queued, k)
				}
				return o, true
			}
		}
	}
	return Op{}, false
}

func (q *Queue) each(f func(o Op)) {
	for _, b := range q.running {
		for _, o := range b.Ops {
			f(o)
		}
	}
	for _, ops := range q.queued {
		for _, o := range ops {
			f(o)
		}
	}
}

// Hidden reports whether a move of this message is queued or running (a
// fresh server list must not show it in its old folder).
func (q *Queue) Hidden(acct int, folder string, uid uint32) bool {
	hit := false
	q.each(func(o Op) {
		if o.Kind == Move && o.Acct == acct && o.Folder == folder && o.UID == uid {
			hit = true
		}
	})
	return hit
}

// FlagState returns the flag value the user last asked for while the
// change is not confirmed (ok=false when nothing is pending).
func (q *Queue) FlagState(acct int, folder string, uid uint32, f backend.Flag) (on, ok bool) {
	var last *Op
	q.each(func(o Op) {
		if o.Kind == Flag && o.Acct == acct && o.Folder == folder && o.UID == uid && o.Flag == f {
			if last == nil || o.ID > last.ID {
				c := o
				last = &c
			}
		}
	})
	if last == nil {
		return false, false
	}
	return last.On, true
}

// MovesInto returns pending moves whose destination is folder.
func (q *Queue) MovesInto(acct int, folder string) []Op {
	var out []Op
	q.each(func(o Op) {
		if o.Kind == Move && o.Acct == acct && o.Dest == folder {
			out = append(out, o)
		}
	})
	return out
}

// Pending is the number of queued + running ops.
func (q *Queue) Pending() int {
	n := 0
	q.each(func(Op) { n++ })
	return n
}

// All returns every op not yet confirmed (running batches first, then the
// queued ones in order), e.g. to keep them across a restart.
func (q *Queue) All() []Op {
	var out []Op
	q.each(func(o Op) { out = append(out, o) })
	return out
}
