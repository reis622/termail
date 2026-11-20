// Package toast holds the in-app notification state.
//
// Two kinds exist:
//   - action toasts ("Silindi · u geri al", "Kaydedildi: ...", errors): short-lived,
//     they disappear automatically after their duration.
//   - new-mail toasts: sticky. They stay until the mail is opened, becomes read
//     (e.g. highlighted in the list) or the user dismisses them. Several new
//     mails are merged into one toast ("3 yeni ileti").
package toast

import (
	"fmt"
	"strings"
	"time"

	"termail/internal/i18n"
)

// Kind of an action toast.
type Kind int

const (
	Info Kind = iota
	Success
	Error
	Deleted // shows the "u geri al" hint
)

// Action is a short-lived toast.
type Action struct {
	ID      int
	Kind    Kind
	Label   string // e.g. "Silindi:"
	Text    string
	Count   int // merged same-kind actions (0/1 = a single toast)
	Expires time.Time
}

// Mail is one unread new message that triggered a sticky toast.
type Mail struct {
	Account     int
	AccountName string // e.g. "İş · Outlook"
	Folder      string
	UID         uint32
	From        string
	Subject     string
	At          time.Time
}

func (m Mail) key() string { return fmt.Sprintf("%d\x00%s\x00%d", m.Account, m.Folder, m.UID) }

// DefaultDuration is how long action toasts stay visible.
const DefaultDuration = 4 * time.Second

// Manager stores toasts. Not safe for concurrent use; it lives in the UI model.
type Manager struct {
	actions []Action
	mails   []Mail // oldest first
	nextID  int
	// MaxActions caps how many action toasts are stacked.
	MaxActions int
}

// Push adds an action toast that expires after d (DefaultDuration if 0).
// Same-kind toasts merge or replace so rapid repeated actions do not stack.
// Errors stay separate but an identical error is only shown once.
func (m *Manager) Push(kind Kind, label, text string, now time.Time, d time.Duration) int {
	if d == 0 {
		d = DefaultDuration
	}
	exp := now.Add(d)
	if kind == Error {
		for i := range m.actions {
			if m.actions[i].Kind == Error && m.actions[i].Text == text {
				m.actions[i].Expires = exp
				return m.actions[i].ID
			}
		}
	} else {
		for i := range m.actions {
			a := &m.actions[i]
			if a.Kind != kind {
				continue
			}
			// Deleted merges only with the same label (Silindi / Taşındı / …).
			if kind == Deleted && a.Label != label && a.Count <= 1 {
				continue
			}
			if kind == Deleted && a.Count > 1 && deletedVerb(a.Label, a.Text) != deletedVerb(label, text) {
				continue
			}
			n := a.Count
			if n < 1 {
				n = 1
			}
			n++
			a.Count, a.Expires = n, exp
			if kind == Deleted {
				a.Text = fmt.Sprintf("%d ileti %s", n, deletedVerb(label, a.Text))
				if a.Label == "" {
					a.Label = label
				}
			} else {
				a.Label, a.Text = label, text
			}
			return a.ID
		}
	}
	m.nextID++
	m.actions = append(m.actions, Action{ID: m.nextID, Kind: kind, Label: label, Text: text, Count: 1, Expires: exp})
	max := m.MaxActions
	if max == 0 {
		max = 3
	}
	if len(m.actions) > max {
		m.actions = m.actions[len(m.actions)-max:]
	}
	return m.nextID
}

func deletedVerb(label, text string) string {
	s := strings.ToLower(label + " " + text)
	switch {
	case strings.Contains(s, "arşiv"):
		return i18n.T("arşivlendi")
	case strings.Contains(s, "taşın"):
		return i18n.T("taşındı")
	case strings.Contains(s, "yükle"):
		return i18n.T("geri yüklendi")
	}
	return "silindi"
}

// SetCount updates a merged action toast after one of its undos.
func (m *Manager) SetCount(id, n int, now time.Time) bool {
	if n < 1 {
		m.Remove(id)
		return true
	}
	for i := range m.actions {
		if m.actions[i].ID == id {
			a := &m.actions[i]
			a.Count = n
			a.Expires = now.Add(DefaultDuration)
			if a.Kind == Deleted {
				a.Text = fmt.Sprintf("%d ileti %s", n, deletedVerb(a.Label, a.Text))
			}
			return true
		}
	}
	return false
}

// Remove drops an action toast by id.
func (m *Manager) Remove(id int) {
	for i, a := range m.actions {
		if a.ID == id {
			m.actions = append(m.actions[:i], m.actions[i+1:]...)
			return
		}
	}
}

// Tick removes expired action toasts. It never touches new-mail toasts.
// It reports whether anything changed.
func (m *Manager) Tick(now time.Time) bool {
	kept := m.actions[:0]
	changed := false
	for _, a := range m.actions {
		if now.Before(a.Expires) {
			kept = append(kept, a)
		} else {
			changed = true
		}
	}
	m.actions = kept
	return changed
}

// Actions returns the visible action toasts (oldest first).
func (m *Manager) Actions() []Action { return m.actions }

// AddMail registers a new unread mail (duplicates are ignored).
func (m *Manager) AddMail(n Mail) {
	for _, x := range m.mails {
		if x.key() == n.key() {
			return
		}
	}
	m.mails = append(m.mails, n)
}

// MarkRead removes the given mail from the sticky toast (opened / read /
// deleted). Reports whether it was part of the toast.
func (m *Manager) MarkRead(account int, folder string, uid uint32) bool {
	k := Mail{Account: account, Folder: folder, UID: uid}.key()
	for i, x := range m.mails {
		if x.key() == k {
			m.mails = append(m.mails[:i], m.mails[i+1:]...)
			return true
		}
	}
	return false
}

// DismissMail closes the new-mail toast entirely.
func (m *Manager) DismissMail() { m.mails = nil }

// HasMail reports whether a new-mail toast is visible. Toast keys (Esc/x/g)
// must only be handled when this is true.
func (m *Manager) HasMail() bool { return len(m.mails) > 0 }

// Mails returns the pending new mails, newest first.
func (m *Manager) Mails() []Mail {
	out := make([]Mail, len(m.mails))
	for i := range m.mails {
		out[i] = m.mails[len(m.mails)-1-i]
	}
	return out
}

// Target is the mail that "göster" jumps to: the newest one.
func (m *Manager) Target() (Mail, bool) {
	if len(m.mails) == 0 {
		return Mail{}, false
	}
	return m.mails[len(m.mails)-1], true
}

// Title is the toast heading: "Yeni ileti" or "3 yeni ileti".
func (m *Manager) Title() string {
	if len(m.mails) <= 1 {
		return i18n.T("Yeni ileti")
	}
	return i18n.Tf("%d yeni ileti", len(m.mails))
}

// Update changes the text of an action toast (progress) and extends it.
func (m *Manager) Update(id int, label, text string, now time.Time, d time.Duration) bool {
	for i := range m.actions {
		if m.actions[i].ID == id {
			m.actions[i].Label, m.actions[i].Text = label, text
			if d == 0 {
				d = DefaultDuration
			}
			m.actions[i].Expires = now.Add(d)
			return true
		}
	}
	return false
}
