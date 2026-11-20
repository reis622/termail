package ui

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/account"
)

type indexItem struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
	Path string `json:"path"`
}

type fileIndex struct {
	mu       sync.RWMutex
	items    []indexItem
	ready    bool
	building bool
	roots    []string
	path     string
}

type indexReadyMsg struct{}

type quickEntry struct {
	label, path string
}

func newFileIndex(roots []string, cachePath string) *fileIndex {
	idx := &fileIndex{roots: roots, path: cachePath}
	idx.loadCache()
	return idx
}

func (idx *fileIndex) loadCache() {
	if idx.path == "" {
		return
	}
	raw, err := os.ReadFile(idx.path)
	if err != nil {
		return
	}
	var items []indexItem
	if json.Unmarshal(raw, &items) != nil {
		return
	}
	idx.mu.Lock()
	idx.items = items
	idx.ready = len(items) > 0
	idx.mu.Unlock()
}

func (idx *fileIndex) saveCache() {
	if idx.path == "" {
		return
	}
	idx.mu.RLock()
	items := append([]indexItem(nil), idx.items...)
	idx.mu.RUnlock()
	raw, err := json.Marshal(items)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(idx.path), 0o700)
	tmp := idx.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) != nil {
		return
	}
	_ = os.Rename(tmp, idx.path)
}

func (idx *fileIndex) start() tea.Cmd {
	idx.mu.Lock()
	if idx.building {
		idx.mu.Unlock()
		return nil
	}
	idx.building = true
	roots := append([]string(nil), idx.roots...)
	idx.mu.Unlock()
	return func() tea.Msg {
		idx.build(roots)
		idx.saveCache()
		idx.mu.Lock()
		idx.building, idx.ready = false, true
		idx.mu.Unlock()
		return indexReadyMsg{}
	}
}

func (idx *fileIndex) build(roots []string) {
	var out []indexItem
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		walkAdd(root, &out, seen)
	}
	idx.mu.Lock()
	idx.items = out
	idx.ready = true
	idx.mu.Unlock()
}

func walkAdd(root string, out *[]indexItem, seen map[string]bool) {
	root, err := filepath.Abs(root)
	if err != nil {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil // permission / vanished file: keep going
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && skipIndexDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		key := strings.ToLower(abs)
		if seen[key] {
			return nil
		}
		seen[key] = true
		*out = append(*out, indexItem{Name: name, Dir: filepath.Dir(abs), Path: abs})
		return nil
	})
}

func skipIndexDir(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "windows", "program files", "program files (x86)", "programdata",
		"appdata", "$recycle.bin", "recycle.bin", "system volume information",
		"node_modules", ".git", "recovery", "winsxs", "boot", "perflogs",
		"system32", "syswow64":
		return true
	}
	return false
}

func (idx *fileIndex) search(q string, limit int) []indexItem {
	q = strings.TrimSpace(strings.ToLower(q))
	if q == "" || idx == nil {
		return nil
	}
	idx.mu.RLock()
	items := idx.items
	idx.mu.RUnlock()
	var pre, has []indexItem
	for _, it := range items {
		n := strings.ToLower(it.Name)
		switch {
		case strings.HasPrefix(n, q):
			pre = append(pre, it)
		case strings.Contains(n, q):
			has = append(has, it)
		}
		if limit > 0 && len(pre) >= limit {
			break
		}
	}
	out := append(pre, has...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (idx *fileIndex) hasData() bool {
	if idx == nil {
		return false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.items) > 0
}

func defaultIndexRoots() []string {
	var roots []string
	for _, e := range userQuickEntries() {
		roots = append(roots, e.path)
	}
	if runtime.GOOS == "windows" {
		home, _ := os.UserHomeDir()
		homeDrive := ""
		if home != "" {
			homeDrive = strings.ToUpper(filepath.VolumeName(home))
		}
		for c := 'A'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if st, err := os.Stat(root); err != nil || !st.IsDir() {
				continue
			}
			// Index the rest of a home drive after the user folders; other
			// drives are walked in full (system dirs are skipped).
			if homeDrive != "" && strings.ToUpper(filepath.VolumeName(root)) == homeDrive {
				continue
			}
			roots = append(roots, root)
		}
	}
	return uniqueExisting(roots)
}

func userQuickEntries() []quickEntry {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	cands := []struct {
		label string
		names []string
	}{
		{"Masaüstü", []string{"Desktop", "Masaüstü"}},
		{"İndirilenler", []string{"Downloads", "İndirilenler"}},
		{"Belgeler", []string{"Documents", "Belgeler"}},
		{"Resimler", []string{"Pictures", "Resimler"}},
	}
	var out []quickEntry
	seen := map[string]bool{}
	for _, c := range cands {
		for _, n := range c.names {
			p := filepath.Join(home, n)
			if !dirExists(p) {
				continue
			}
			abs, _ := filepath.Abs(p)
			key := strings.ToLower(abs)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, quickEntry{label: c.label, path: abs})
			break
		}
	}
	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			root := string(c) + `:\`
			if st, err := os.Stat(root); err == nil && st.IsDir() {
				out = append(out, quickEntry{label: root, path: root})
			}
		}
	}
	return out
}

func uniqueExisting(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range in {
		if !dirExists(p) {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		key := strings.ToLower(abs)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, abs)
	}
	return out
}

func looksLikePath(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if strings.ContainsAny(v, `/\`) {
		return true
	}
	if len(v) >= 2 && unicode.IsLetter(rune(v[0])) && v[1] == ':' {
		return true
	}
	if strings.HasPrefix(v, "~") {
		return true
	}
	return false
}

func defaultIndexCachePath() string {
	dir, err := account.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "fileindex.json")
}
