package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/mailmsg"
)

// benchModel: one Gmail-like account, N mails in the inbox, every body
// already cached (so the reading pane renders text on every keypress).
func benchModel(n int) *Model {
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	now := time.Now()
	mem := backend.NewMemory([]backend.Folder{{Name: "INBOX", Role: backend.RoleInbox, Total: n}, {Name: "[Gmail]/Sent Mail", Role: backend.RoleSent}, {Name: "[Gmail]/Trash", Role: backend.RoleTrash}})
	a := &Acct{Cfg: account.Account{Name: "Gmail", Email: "ahmet@gmail.com"}, B: mem, Folders: []backend.Folder{{Name: "INBOX", Role: backend.RoleInbox, Total: n}, {Name: "[Gmail]/Sent Mail", Role: backend.RoleSent}, {Name: "[Gmail]/Trash", Role: backend.RoleTrash}}}
	m := New(Options{Accounts: []*Acct{a}})
	m.applySize(160, 40)
	var l []backend.Envelope
	body := strings.Repeat("Merhaba Ahmet, toplantı notlarını ekte bulabilirsin. Çarşamba günü İstanbul ofisinde görüşelim; şimdiden teşekkürler. ", 40)
	for i := 0; i < n; i++ {
		e := backend.Envelope{UID: uint32(n - i), From: "Gönderen Kişi " + strconv.Itoa(i), FromAddr: "kisi" + strconv.Itoa(i) + "@ornek.com",
			Subject: "Konu: proje güncellemesi ve haftalık özet raporu #" + strconv.Itoa(i), Date: now.Add(-time.Duration(i) * time.Hour), Seen: i%3 != 0, Flagged: i%7 == 0, Attach: i%5 == 0}
		l = append(l, e)
		m.bodies.Put(bodyKey{0, "INBOX", e.UID}, &mailmsg.Parsed{Subject: e.Subject, Text: body + strconv.Itoa(i)})
	}
	a.Lists["INBOX"], a.Loaded["INBOX"] = l, true
	m.folder = "INBOX"
	m.cursor = 500
	m.View()
	return m
}

// BenchmarkKeypress measures one ↓/↑ keypress: Update + View at 160x40.
func BenchmarkKeypress(b *testing.B) {
	m := benchModel(2000)
	down, up := key("down"), key("up")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := down
		if i%40 >= 20 {
			k = up
		}
		m.Update(k)
		_ = m.View()
	}
}

// TestKeypressBudget keeps the hot path honest in normal test runs.
func TestKeypressBudget(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	r := testing.Benchmark(BenchmarkKeypress)
	ms := float64(r.NsPerOp()) / 1e6
	t.Logf("keypress (Update+View, 2000 mails, 160x40): %.3f ms, %d allocs", ms, r.AllocsPerOp())
	if ms > 5 {
		t.Fatalf("keypress takes %.2f ms (budget 5 ms)", ms)
	}
}
