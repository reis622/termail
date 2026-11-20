package localstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxBodyBytes = 4 << 20  // skip a huge message rather than fill the disk
	maxBodyStore = 30 << 20 // prune the cache when it grows past this
	maxBodyFiles = 40
)

func (s *Store) bodiesDir() string { return filepath.Join(s.cacheDir(), "govde") }

func bodyName(email, folder string, uid uint32) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email)) + "\x00" + folder))
	return fmt.Sprintf("%s-%d.eml", hex.EncodeToString(h[:8]), uid)
}

// LoadBody returns a cached raw message, or nil.
func (s *Store) LoadBody(email, folder string, uid uint32) []byte {
	if !s.ok() || uid == 0 {
		return nil
	}
	b, err := readSmall(filepath.Join(s.bodiesDir(), bodyName(email, folder, uid)))
	if err != nil || len(b) == 0 {
		return nil
	}
	return b
}

// SaveBody stores a raw message and prunes older ones when the cache is full.
func (s *Store) SaveBody(email, folder string, uid uint32, raw []byte) error {
	if !s.ok() || uid == 0 || len(raw) == 0 || len(raw) > maxBodyBytes {
		return nil
	}
	dir := s.bodiesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, bodyName(email, folder, uid))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeAtomic(path, raw); err != nil {
		return err
	}
	s.pruneBodiesLocked(dir)
	return nil
}

func (s *Store) pruneBodiesLocked(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		mod  time.Time
		size int64
	}
	var all []file
	var sum int64
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		all = append(all, file{filepath.Join(dir, e.Name()), info.ModTime(), info.Size()})
		sum += info.Size()
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) }) // oldest first
	for i := 0; i < len(all) && (len(all)-i > maxBodyFiles || sum > maxBodyStore); i++ {
		sum -= all[i].size
		_ = os.Remove(all[i].path)
	}
}
