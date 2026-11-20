package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/emersion/go-message/mail"

	"termail/internal/account"
)

// recipEntry is one remembered recipient from successfully sent mail.
type recipEntry struct {
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Count    int       `json:"count"`
	LastUsed time.Time `json:"last_used"`
}

type recipBook struct {
	Items []recipEntry `json:"items"`
	path  string
}

func (m *Model) recipBook() *recipBook {
	if m.recip != nil {
		return m.recip
	}
	dir, err := account.Dir()
	if err != nil {
		m.recip = &recipBook{}
		return m.recip
	}
	m.recip = loadRecipBook(dir)
	return m.recip
}

func loadRecipBook(dir string) *recipBook {
	b := &recipBook{path: filepath.Join(dir, "recipients.json")}
	raw, err := os.ReadFile(b.path)
	if err != nil {
		return b
	}
	_ = json.Unmarshal(raw, b)
	b.path = filepath.Join(dir, "recipients.json")
	return b
}

func (b *recipBook) save() error {
	if b == nil || b.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}

func (b *recipBook) remember(addrs []*mail.Address, now time.Time) {
	if b == nil {
		return
	}
	for _, a := range addrs {
		if a == nil {
			continue
		}
		email := strings.TrimSpace(a.Address)
		if email == "" || !strings.Contains(email, "@") {
			continue
		}
		key := strings.ToLower(email)
		found := false
		for i := range b.Items {
			if strings.ToLower(b.Items[i].Email) != key {
				continue
			}
			b.Items[i].Count++
			b.Items[i].LastUsed = now
			if strings.TrimSpace(a.Name) != "" {
				b.Items[i].Name = a.Name
			}
			found = true
			break
		}
		if !found {
			b.Items = append(b.Items, recipEntry{Name: a.Name, Email: email, Count: 1, LastUsed: now})
		}
	}
}

func (b *recipBook) suggest(q string, limit int) []recipEntry {
	q = strings.TrimSpace(strings.ToLower(q))
	if q == "" || b == nil {
		return nil
	}
	var pre, has []recipEntry
	for _, e := range b.Items {
		name, email := strings.ToLower(e.Name), strings.ToLower(e.Email)
		switch {
		case strings.HasPrefix(name, q) || strings.HasPrefix(email, q):
			pre = append(pre, e)
		case strings.Contains(name, q) || strings.Contains(email, q):
			has = append(has, e)
		}
	}
	sortRecip(pre)
	sortRecip(has)
	out := append(pre, has...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sortRecip(s []recipEntry) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j].Count > s[i].Count || s[j].Count == s[i].Count && s[j].LastUsed.After(s[i].LastUsed) {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func formatRecip(e recipEntry) string {
	if strings.TrimSpace(e.Name) != "" {
		a := &mail.Address{Name: e.Name, Address: e.Email}
		return a.String()
	}
	return e.Email
}

// addrToken returns the address fragment being typed around the cursor.
func addrToken(s string, col int) (start, end int, token string) {
	rs := []rune(s)
	if col < 0 {
		col = 0
	}
	if col > len(rs) {
		col = len(rs)
	}
	start, end = col, col
	for start > 0 && rs[start-1] != ',' && rs[start-1] != ';' {
		start--
	}
	for end < len(rs) && rs[end] != ',' && rs[end] != ';' {
		end++
	}
	for start < end && unicode.IsSpace(rs[start]) {
		start++
	}
	for end > start && unicode.IsSpace(rs[end-1]) {
		end--
	}
	return start, end, string(rs[start:end])
}

func replaceAddrToken(s string, col int, repl string) string {
	start, end, _ := addrToken(s, col)
	rs := []rune(s)
	out := string(rs[:start]) + repl
	rest := string(rs[end:])
	if !strings.HasPrefix(strings.TrimLeft(rest, " "), ",") && !strings.HasPrefix(strings.TrimLeft(rest, " "), ";") {
		if rest != "" && !strings.HasPrefix(rest, " ") {
			out += ", "
		} else if rest == "" {
			out += ", "
		}
	}
	return out + rest
}

var _ = time.Time{}
