package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"termail/internal/account"
	"termail/internal/backend"
	"termail/internal/mailmsg"
	"termail/internal/samplemail"
	"termail/internal/toast"
)

// nastyModel is a model full of hostile text: emoji, variation selectors,
// Turkish letters (incl. decomposed İ), CJK, tabs, carriage returns,
// newlines, control and zero-width characters, very long subjects.
func nastyModel(t *testing.T) *Model {
	t.Helper()
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)
	folders := []backend.Folder{{Name: "INBOX", Role: backend.RoleInbox, Total: 71, Unread: 52}, {Name: "[Gmail]/Sent Mail", Role: backend.RoleSent},
		{Name: "[Gmail]/Drafts", Role: backend.RoleDrafts, Total: 1}, {Name: "[Gmail]/Spam", Role: backend.RoleJunk}, {Name: "[Gmail]/Trash", Role: backend.RoleTrash},
		{Name: "Faturalar 🧾\tözel\r\nklasör", Role: backend.RoleNone, Unread: 3}}
	var accts []*Acct
	for i, name := range []string{"Gmail", "İş 💼 hesabı çok uzun bir ad ile"} {
		mem := backend.NewMemory(folders)
		a := &Acct{Cfg: account.Account{Name: name, Email: fmt.Sprintf("ahmet%d@gmail.com", i)}, B: mem, Folders: append([]backend.Folder(nil), folders...), Open: true}
		accts = append(accts, a)
	}
	m := New(Options{Accounts: accts, Now: func() time.Time { return now }})
	odd := []backend.Envelope{
		{From: "İstanbul İl Ambalaj Atık Yönetimi Müdürlüğü", Subject: "Ynt: " + strings.Repeat("Google Play'de satın alma işlemlerini doğrulama ayarlarındaki değişiklikler ", 3)},
		{From: "Google 🎉\u200d🚀", Subject: "☑️ Ali, finish setting up your Windows with Google ❤️‍🔥 👩‍👩‍👧‍👦 🇹🇷"},
		{From: "山田\t太郎", Subject: "会議のお知らせ\t– 東京オフィス\r\nİstanbul\x07ekibi\x1b[31mKIRMIZI"},
		{From: "Ayşe", Subject: "I\u0307stanbul ve Çağrı (decomposed) e\u0301 \u200b\u200e\ufeff gizli"},
		{From: "", FromAddr: "noreply@very-long-domain-name-for-testing.example.com.tr", Subject: ""},
		{From: "Tab\there", Subject: "\t\t\tbaşta sekmeler", Attach: true, Flagged: true},
	}
	for ai, a := range accts {
		var l []backend.Envelope
		for i := 0; i < 80; i++ {
			e := odd[i%len(odd)]
			e.UID = uint32(1000 - i)
			e.Date = now.Add(-time.Duration(i) * time.Hour)
			e.Seen = i%3 == 0
			l = append(l, e)
		}
		for i, s := range samplemail.All(now) {
			l = append(l, backend.Envelope{UID: uint32(2000 + i), From: s.FromName, FromAddr: s.FromAddr, Subject: s.Subject, Date: s.Date, Attach: s.Attach})
			if p, err := mailmsg.Parse(s.Raw); err == nil {
				m.bodies.Put(bodyKey{ai, "INBOX", uint32(2000 + i)}, p)
			}
		}
		a.Lists["INBOX"] = l
		a.Loaded["INBOX"] = true
		a.Saved = l[:5]
	}
	return m
}

var sizes = [][2]int{{120, 20}, {80, 24}, {60, 15}, {200, 50}, {100, 25}, {160, 40}, {40, 12}, {30, 10}, {41, 13}, {137, 33}}

var ctrlRe = regexp.MustCompile("[\x00-\x08\x0b-\x1f\x7f]")

func checkFrame(t *testing.T, label string, m *Model) {
	t.Helper()
	v := m.View()
	if strings.HasSuffix(v, "\n") {
		t.Fatalf("%s: frame ends with a newline", label)
	}
	lines := strings.Split(v, "\n")
	if len(lines) != m.realH {
		t.Fatalf("%s: %d lines, window has %d", label, len(lines), m.realH)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.realW-edgeMargin {
			t.Fatalf("%s: line %d is %d cells (window %d): %q", label, i, w, m.realW, ansi.Strip(l))
		}
		plain := ansi.Strip(l)
		if ctrlRe.MatchString(plain) {
			t.Fatalf("%s: control character on line %d: %q", label, i, plain)
		}
		for _, r := range plain {
			if r >= 0x1f000 || (r >= 0xfe00 && r <= 0xfe0f) || r == 0x200d || r == 0x200b {
				t.Fatalf("%s: unsafe rune %U on line %d: %q", label, r, i, plain)
			}
		}
	}
}

func press(m *Model, k string) {
	var msg tea.KeyMsg
	switch k {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "pgdown":
		msg = tea.KeyMsg{Type: tea.KeyPgDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	m.Update(msg)
}

func TestFrameAlwaysFitsWindow(t *testing.T) {
	scenarios := []struct {
		name string
		keys []string
		prep func(m *Model)
	}{
		{name: "list"},
		{name: "folders", keys: []string{"left", "down", "down", "down", "up", "down", "down", "down", "down", "down", "down"}},
		{name: "unified", keys: []string{"0", "down", "down"}},
		{name: "saved", prep: func(m *Model) { m.showSaved() }},
		{name: "reader", keys: []string{"down", "enter", "pgdown"}},
		{name: "wide", keys: []string{"enter", "z"}},
		{name: "toasts", prep: func(m *Model) {
			for i := 0; i < 4; i++ {
				m.toasts.AddMail(toast.Mail{Account: 0, AccountName: "Gmail · Gmail 🎉", Folder: "INBOX", UID: uint32(1000 - i), From: "Google 🎉\tPlay", Subject: "☑️ uzun konu " + strings.Repeat("çok ", 30), At: m.opt.Now()})
			}
		}},
		{name: "help", keys: []string{"?"}},
		{name: "readtab", prep: func(m *Model) {
			if e, ok := m.selected(); ok {
				ai, fo := m.loc(e)
				m.openReader(ai, fo, e)
			}
		}},
		{name: "compose", keys: []string{"r"}},
		{name: "samples", prep: func(m *Model) { m.cursor = 80 }, keys: []string{"down", "down", "enter", "down", "esc", "left", "down"}},
	}
	for _, sz := range sizes {
		for _, sc := range scenarios {
			label := fmt.Sprintf("%dx%d/%s", sz[0], sz[1], sc.name)
			m := nastyModel(t)
			m.applySize(sz[0], sz[1])
			if sc.prep != nil {
				sc.prep(m)
			}
			checkFrame(t, label+"/start", m)
			for i, k := range sc.keys {
				press(m, k)
				checkFrame(t, fmt.Sprintf("%s/key%d(%s)", label, i, k), m)
			}
			// a resize in the middle of it
			m.Update(tea.WindowSizeMsg{Width: sz[1] + 50, Height: sz[0]/4 + 5})
			checkFrame(t, label+"/resized", m)
		}
	}
}

func TestFrameBeforeSizeKnownIsSmall(t *testing.T) {
	termSizeFn = func() (int, int, bool) { return 0, 0, false }
	m := New(Options{})
	if n := strings.Count(m.View(), "\n") + 1; n > 24 {
		t.Fatalf("%d lines before the window size is known", n)
	}
}

func TestClampFrame(t *testing.T) {
	got := clampFrame("abcdef\n\x1b[31mçğüşöı\x1b[0m\nx\ny", 4, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 || ansi.Strip(lines[0]) != "abcd" || ansi.Strip(lines[1]) != "çğüş" {
		t.Fatalf("%q", got)
	}
	if got := clampFrame("a", 5, 3); strings.Count(got, "\n") != 2 || strings.HasSuffix(got, "x") {
		t.Fatalf("%q", got)
	}
}
