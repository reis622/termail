package localstore

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

// MaxRecent is how many recent searches are kept.
const MaxRecent = 10

// Prefs are small view settings remembered between runs. Recent holds
// only what was typed into the search bar (never mail text).
type Prefs struct {
	V         int              `json:"v"`
	Recent    []string         `json:"son_aramalar,omitempty"`
	Grouped   bool             `json:"gonderene_gore,omitempty"`
	Open      map[string]bool  `json:"acik_hesaplar,omitempty"` // account address -> expanded in the folder panel
	GroupSeen map[string]int64 `json:"grup_bakildi,omitempty"`  // sender group -> newest mail date seen (unix)
	Saved     time.Time        `json:"kaydedildi"`
}

func (s *Store) prefsPath() string { return filepath.Join(s.Dir, "arayuz.json") }

// LoadPrefs reads the settings (zero value when there are none).
func (s *Store) LoadPrefs() Prefs {
	var p Prefs
	if !s.ok() {
		return p
	}
	b, err := readSmall(s.prefsPath())
	if err != nil || json.Unmarshal(b, &p) != nil || p.V != Version {
		return Prefs{}
	}
	if len(p.Recent) > MaxRecent {
		p.Recent = p.Recent[:MaxRecent]
	}
	return p
}

// SavePrefs writes the settings atomically.
func (s *Store) SavePrefs(p Prefs) error {
	if !s.ok() {
		return nil
	}
	p.V, p.Saved = Version, time.Now()
	if len(p.Recent) > MaxRecent {
		p.Recent = p.Recent[:MaxRecent]
	}
	if len(p.GroupSeen) > 500 { // forget the oldest groups
		type kv struct {
			k string
			v int64
		}
		var all []kv
		for k, v := range p.GroupSeen {
			all = append(all, kv{k, v})
		}
		for len(all) > 500 {
			oldest := 0
			for i := range all {
				if all[i].v < all[oldest].v {
					oldest = i
				}
			}
			all = append(all[:oldest], all[oldest+1:]...)
		}
		p.GroupSeen = map[string]int64{}
		for _, x := range all {
			p.GroupSeen[x.k] = x.v
		}
	}
	b, err := json.MarshalIndent(p, "", " ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeAtomic(s.prefsPath(), b)
}

// AddRecent puts q first in the list of recent searches (no duplicates,
// ignoring case; at most MaxRecent).
func AddRecent(list []string, q string) []string {
	q = strings.Join(strings.Fields(q), " ")
	if q == "" {
		return list
	}
	out := []string{q}
	for _, x := range list {
		if !strings.EqualFold(x, q) && len(out) < MaxRecent {
			out = append(out, x)
		}
	}
	return out
}
