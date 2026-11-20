// Package blocklist keeps blocked senders (addresses or whole domains) per
// account in a local JSON file. It never contains credentials.
package blocklist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
	"termail/internal/i18n"
)

// Kind of a block entry.
type Kind string

const (
	Address Kind = "address"
	Domain  Kind = "domain"
)

// Entry is one blocked sender.
type Entry struct {
	Account string    `json:"account"` // account e-mail address the rule belongs to
	Kind    Kind      `json:"kind"`
	Value   string    `json:"value"` // "spam@reklam.com" or "reklam.com"
	Added   time.Time `json:"added"`
}

// Label is the user-facing kind name.
func (e Entry) Label() string {
	if e.Kind == Domain {
		return i18n.T("Alan adı")
	}
	return "Adres"
}

// Display is "@reklam.com" for domains, the address otherwise.
func (e Entry) Display() string {
	if e.Kind == Domain {
		return "@" + e.Value
	}
	return e.Value
}

// List is the block list. Safe for concurrent use.
type List struct {
	mu      sync.Mutex
	path    string
	entries []Entry
}

type fileFormat struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Load reads path; a missing file gives an empty list. An empty path keeps
// the list in memory only (demo mode).
func Load(path string) (*List, error) {
	l := &List{path: path}
	if path == "" {
		return l, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	var f fileFormat
	if err := json.Unmarshal(b, &f); err != nil {
		return l, err
	}
	l.entries = f.Entries
	return l, nil
}

// Save writes the file atomically (no-op for in-memory lists).
func (l *List) Save() error {
	l.mu.Lock()
	f := fileFormat{Version: 1, Entries: append([]Entry(nil), l.entries...)}
	path := l.path
	l.mu.Unlock()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Normalize lower-cases and trims an address or domain ("@x.com" -> "x.com").
func Normalize(kind Kind, v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.Trim(v, "<>")
	if kind == Domain {
		if i := strings.LastIndex(v, "@"); i >= 0 {
			v = v[i+1:]
		}
		v = strings.Trim(v, ".")
	}
	return v
}

// DomainOf returns the domain part of an address.
func DomainOf(addr string) string { return Normalize(Domain, addr) }

// BaseDomain returns the registrable domain of an address or domain
// ("kampanya@bulten.reklam.com" -> "reklam.com", "x@a.firma.com.tr" -> "firma.com.tr").
// Blocking a domain uses this so all of the sender's subdomains are covered.
func BaseDomain(addr string) string {
	d := DomainOf(addr)
	if b, err := publicsuffix.EffectiveTLDPlusOne(d); err == nil {
		return b
	}
	return d
}

func sameAcct(a, b string) bool { return strings.EqualFold(a, b) }

// Add blocks value for account. Returns the entry and whether it was new.
func (l *List) Add(account string, kind Kind, value string, now time.Time) (Entry, bool) {
	value = Normalize(kind, value)
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if sameAcct(e.Account, account) && e.Kind == kind && e.Value == value {
			return e, false
		}
	}
	e := Entry{Account: strings.ToLower(account), Kind: kind, Value: value, Added: now}
	l.entries = append(l.entries, e)
	return e, true
}

// Remove deletes a block. Reports whether something was removed.
func (l *List) Remove(account string, kind Kind, value string) bool {
	value = Normalize(kind, value)
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, e := range l.entries {
		if sameAcct(e.Account, account) && e.Kind == kind && e.Value == value {
			l.entries = append(l.entries[:i], l.entries[i+1:]...)
			return true
		}
	}
	return false
}

// Match reports whether from (an address) is blocked for account. Domain
// rules match the exact domain and its subdomains, case-insensitively.
func (l *List) Match(account, from string) (Entry, bool) {
	addr := Normalize(Address, from)
	if addr == "" {
		return Entry{}, false
	}
	dom := DomainOf(addr)
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if !sameAcct(e.Account, account) {
			continue
		}
		switch e.Kind {
		case Address:
			if addr == e.Value {
				return e, true
			}
		case Domain:
			if dom == e.Value || strings.HasSuffix(dom, "."+e.Value) {
				return e, true
			}
		}
	}
	return Entry{}, false
}

// All returns every entry, newest first.
func (l *List) All() []Entry {
	l.mu.Lock()
	out := append([]Entry(nil), l.entries...)
	l.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Added.After(out[j].Added) })
	return out
}
