package backend

import "testing"

func TestBatches(t *testing.T) {
	var uids []uint32
	for i := 1; i <= 7; i++ {
		uids = append(uids, uint32(i))
	}
	b := Batches(uids, 3)
	if len(b) != 3 || len(b[0]) != 3 || len(b[2]) != 1 || b[2][0] != 7 {
		t.Fatalf("%v", b)
	}
	b[0] = append(b[0], 99) // must not overwrite the next batch
	if b[1][0] != 4 {
		t.Fatal("batches share memory")
	}
	if Batches(nil, 3) != nil || len(Batches(uids, 0)) != 1 {
		t.Fatal("edge cases")
	}
}
