// Package bodycache is a small least-recently-used cache for message bodies.
package bodycache

import "container/list"

// LRU keeps at most Cap entries. Entries for which Pinned returns true
// (for example mails open in a tab) are never evicted.
type LRU[K comparable, V any] struct {
	Cap    int
	Pinned func(K) bool
	ll     *list.List
	items  map[K]*list.Element
}

type entry[K comparable, V any] struct {
	k K
	v V
}

// New returns an empty cache.
func New[K comparable, V any](capacity int) *LRU[K, V] {
	return &LRU[K, V]{Cap: capacity, ll: list.New(), items: map[K]*list.Element{}}
}

// Get returns the value and marks it as recently used.
func (c *LRU[K, V]) Get(k K) (V, bool) {
	if el, ok := c.items[k]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*entry[K, V]).v, true
	}
	var zero V
	return zero, false
}

// Peek returns the value without touching its position.
func (c *LRU[K, V]) Peek(k K) (V, bool) {
	if el, ok := c.items[k]; ok {
		return el.Value.(*entry[K, V]).v, true
	}
	var zero V
	return zero, false
}

// Has reports whether k is cached without touching its position.
func (c *LRU[K, V]) Has(k K) bool { _, ok := c.items[k]; return ok }

// Put stores a value and evicts the oldest unpinned entries over capacity.
func (c *LRU[K, V]) Put(k K, v V) {
	if el, ok := c.items[k]; ok {
		el.Value.(*entry[K, V]).v = v
		c.ll.MoveToFront(el)
	} else {
		c.items[k] = c.ll.PushFront(&entry[K, V]{k, v})
	}
	for el := c.ll.Back(); el != nil && el != c.ll.Front() && c.ll.Len() > c.Cap; { // never evict the newest
		prev := el.Prev()
		e := el.Value.(*entry[K, V])
		if c.Pinned == nil || !c.Pinned(e.k) {
			c.ll.Remove(el)
			delete(c.items, e.k)
		}
		el = prev
	}
}

// Delete removes k.
func (c *LRU[K, V]) Delete(k K) {
	if el, ok := c.items[k]; ok {
		c.ll.Remove(el)
		delete(c.items, k)
	}
}

// Len is the number of cached entries.
func (c *LRU[K, V]) Len() int { return c.ll.Len() }
