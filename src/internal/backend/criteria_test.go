package backend

import (
	"context"
	"testing"
	"time"
)

func TestMemorySearchCriteria(t *testing.T) {
	m := NewMemory([]Folder{{Name: "INBOX", Role: RoleInbox}})
	now := time.Now()
	m.Add("INBOX", Envelope{From: "Ayşe Demir", FromAddr: "ayse@firma.com", Subject: "Proje", Date: now, Attach: true}, []byte("Subject: Proje\r\n\r\nToplantı notları ektedir.\r\n"))
	m.Add("INBOX", Envelope{From: "Mehmet", FromAddr: "m@x.com", Subject: "Akşam", Date: now.Add(-48 * time.Hour), Seen: true}, []byte("Subject: Akşam\r\n\r\nYemek?\r\n"))
	ctx := context.Background()
	for _, tc := range []struct {
		c    Criteria
		want int
	}{
		{Criteria{Text: []string{"toplanti"}}, 1}, // Turkish letters / case ignored, text searched
		{Criteria{From: []string{"AYŞE"}}, 1},
		{Criteria{Attach: true}, 1},
		{Criteria{Unseen: true}, 1},
		{Criteria{Since: now.Add(-time.Hour)}, 1},
		{Criteria{Before: now.Add(-24 * time.Hour)}, 1},
		{Criteria{Text: []string{"akşam"}, From: []string{"mehmet"}}, 1},
		{Criteria{Text: []string{"yok"}}, 0},
		{Criteria{}, 2},
	} {
		res, err := SearchBy(ctx, m, "INBOX", tc.c, 10)
		if err != nil || len(res) != tc.want {
			t.Errorf("%+v: got %d (%v), want %d", tc.c, len(res), err, tc.want)
		}
	}
	m.SearchDelay = time.Second
	cctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if _, err := m.SearchCriteria(cctx, "INBOX", Criteria{}, 10); err == nil {
		t.Fatal("cancel ignored")
	}
}
