package bodycache

import "testing"

func TestLRU(t *testing.T) {
	c := New[int, string](2)
	c.Put(1, "a")
	c.Put(2, "b")
	c.Get(1) // 1 is now newest
	c.Put(3, "c")
	if c.Has(2) || !c.Has(1) || !c.Has(3) || c.Len() != 2 {
		t.Fatalf("eviction wrong: has1=%v has2=%v has3=%v", c.Has(1), c.Has(2), c.Has(3))
	}
	c.Put(3, "c2")
	if v, _ := c.Get(3); v != "c2" {
		t.Fatal("update failed")
	}
	c.Delete(1)
	if c.Has(1) || c.Len() != 1 {
		t.Fatal("delete failed")
	}
}

func TestPinned(t *testing.T) {
	c := New[int, string](1)
	c.Pinned = func(k int) bool { return k == 1 }
	c.Put(1, "open in a tab")
	c.Put(2, "b")
	c.Put(3, "c")
	if !c.Has(1) || c.Has(2) || !c.Has(3) {
		t.Fatalf("pinned entry evicted or wrong eviction: %v %v %v", c.Has(1), c.Has(2), c.Has(3))
	}
}
