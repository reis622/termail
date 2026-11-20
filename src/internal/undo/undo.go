// Package undo keeps the last delete operations so "u" can bring them back.
package undo

import "sync"

// Limit is how many delete operations are remembered.
const Limit = 10

// Op is the kind of operation that can be undone.
type Op int

const (
	OpDelete Op = iota // moved to the trash
	OpBlock            // sender blocked and message moved to spam
	OpMove             // archived or moved to another folder (Trash is the dest)
	OpFlag             // saved / read / unread
	OpCopy             // inbox copied to a user folder (undo deletes the copy)
)

// Entry describes one message moved to the trash (or to spam by a block).
type Entry struct {
	Op        Op
	ID        int
	Account   int    // account index
	From      string // original folder
	Trash     string // trash folder
	OrigUID   uint32 // UID in the original folder
	TrashUID  uint32 // UID in the trash (0 while the move is still running)
	MessageID string // used as a fallback to find the message in the trash
	Subject   string
	Pending   bool // move not confirmed by server yet
	Data      any  // UI payload (e.g. the envelope) to re-insert optimistically
	// For OpBlock: the rule to remove on undo.
	BlockAccount string
	BlockKind    string
	BlockValue   string
	BlockAdded   bool // false if the rule already existed (undo then keeps it)
	// For OpFlag:
	Flag   string // backend.Flag, e.g. `\Flagged` or `\Seen`
	FlagOn bool   // the value that was applied (undo flips it)
}

// Stack is a bounded LIFO of delete operations. Safe for concurrent use.
type Stack struct {
	mu     sync.Mutex
	items  []Entry
	nextID int
}

// Push records a new delete (Pending=true) and returns its id.
func (s *Stack) Push(e Entry) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	e.ID = s.nextID
	e.Pending = true
	s.items = append(s.items, e)
	if len(s.items) > Limit {
		s.items = append([]Entry(nil), s.items[len(s.items)-Limit:]...)
	}
	return e.ID
}

// Confirm marks the move as done and stores the new UID in the trash.
func (s *Stack) Confirm(id int, trashUID uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].TrashUID = trashUID
			s.items[i].Pending = false
			return true
		}
	}
	return false
}

// Drop removes an entry (e.g. the move failed and was rolled back).
func (s *Stack) Drop(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return
		}
	}
}

// Peek returns the most recent entry.
func (s *Stack) Peek() (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return Entry{}, false
	}
	return s.items[len(s.items)-1], true
}

// Pop removes and returns the most recent entry. If it is still pending,
// it is left in place and ok=false, pending=true is returned.
func (s *Stack) Pop() (e Entry, ok bool, pending bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return Entry{}, false, false
	}
	last := s.items[len(s.items)-1]
	if last.Pending {
		return last, false, true
	}
	s.items = s.items[:len(s.items)-1]
	return last, true, false
}

// Len returns the number of remembered operations.
func (s *Stack) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// DropWhere forgets every entry for which match is true (e.g. all deletes
// whose copy lived in a trash folder that was just emptied).
func (s *Stack) DropWhere(match func(Entry) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.items[:0]
	for _, e := range s.items {
		if !match(e) {
			kept = append(kept, e)
		}
	}
	s.items = kept
}

// Has reports whether an entry is still on the stack.
func (s *Stack) Has(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.items {
		if e.ID == id {
			return true
		}
	}
	return false
}
