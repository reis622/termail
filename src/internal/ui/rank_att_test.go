package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/backend"
	"termail/internal/mailmsg"
)

func rankModel(t *testing.T) *Model {
	m := groupedModel(t)
	now := m.opt.Now()
	m.accts[0].Lists["INBOX"] = []backend.Envelope{
		{UID: 3, From: "Veli", FromAddr: "veli@x.com", Subject: "Merhaba", Date: now.Add(-30 * time.Minute)},
		{UID: 2, From: "Ali", FromAddr: "ali@x.com", Subject: "Fatura geldi", Date: now.Add(-1 * time.Hour)},
		{UID: 4, From: "Can", FromAddr: "can@x.com", Subject: "FATURA 2", Date: now.Add(-3 * time.Hour)},
		{UID: 1, From: "Fatura Merkezi", FromAddr: "bilgi@x.com", Subject: "Bilgi", Date: now.Add(-5 * time.Hour)},
	}
	m.bodies.Put(bodyKey{0, "INBOX", 3}, &mailmsg.Parsed{Text: "ekte faturanız var"})
	return m
}

func uids(v []backend.Envelope) []uint32 {
	var out []uint32
	for _, e := range v {
		out = append(out, e.UID)
	}
	return out
}

func TestSearchRanking(t *testing.T) {
	m := rankModel(t)
	press(m, "/")
	typeText(m, "fatura")
	if got := uids(m.visible()); len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 4 || got[3] != 3 {
		t.Fatalf("order %v, want sender, subject (newest first), body", got)
	}
	m.cursor = 1 // Ali
	// a later server answer: an older sender match goes into the first tier,
	// the selected mail stays selected
	m.Update(searchDoneMsg{seq: m.search.seq, envs: []backend.Envelope{
		{UID: 9, Acct: 0, Folder: "INBOX", From: "Fatura Bot", FromAddr: "bot@x.com", Subject: "x", Date: m.opt.Now().Add(-48 * time.Hour)},
	}})
	got := uids(m.visible())
	if len(got) != 5 || got[0] != 1 || got[1] != 9 || got[2] != 2 {
		t.Fatalf("after server: %v", got)
	}
	if e, ok := m.selected(); !ok || e.UID != 2 {
		t.Fatalf("selection moved to %v", e.UID)
	}
}

func TestAttachmentKey(t *testing.T) {
	m := rankModel(t)
	m.bodies.Put(bodyKey{0, "INBOX", 2}, &mailmsg.Parsed{Text: "x", Attachments: []mailmsg.Attachment{{Name: "a.pdf"}, {Name: "b.pdf"}}})
	selectUID(t, m, 2) // Ali, two attachments
	press(m, "E")
	if m.focus != focusReader || m.pv == nil || m.pv.att != 0 || m.pv.scroll != 0 {
		t.Fatal("E should open the mail at its attachments")
	}
	press(m, "down")
	press(m, "E") // again inside: back to the first attachment, top
	if m.pv.att != 0 || m.pv.scroll != 0 {
		t.Fatalf("inside: att %d scroll %d", m.pv.att, m.pv.scroll)
	}
	press(m, "esc") // leaves attachment mode
	press(m, "esc") // back to the list
	m.cursor = 0    // Veli: cached body, no attachments
	press(m, "E")
	if !hasToast(m, "Bu iletide ek ya da bağlantı yok") {
		t.Fatal("no-attachment toast missing")
	}
	_ = tea.KeyMsg{}
}
