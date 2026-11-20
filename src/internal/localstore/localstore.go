// Package localstore keeps small local files that make Termail start fast
// and exit fast without losing anything:
//
//   - a cache of the last inbox list per account (headers, flags, UID,
//     last-read time; never passwords or codes), shown at once on the
//     next start while the server connects;
//   - recently opened message bodies, so a mail already read opens at once;
//   - the server changes (moves, flags) that were still waiting when the
//     program closed, replayed on the next start;
//   - messages that were being written or sent when the window was closed,
//     reopened on the next start.
//
// Every file is versioned, written atomically (temp file + rename) and
// simply ignored when it is unreadable, corrupt or from another version.
package localstore

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Version of the file formats; files of other versions are ignored.
const Version = 2

// VersionLegacy is the pre-gzip inbox cache (plain JSON).
const VersionLegacy = 1

const maxFile = 16 << 20 // anything bigger is not ours / corrupt

// Base is the folder for all local files: %LOCALAPPDATA%\Termail on
// Windows (~/.cache/Termail elsewhere). TERMAIL_CACHE_DIR overrides it.
func Base() (string, error) {
	if d := os.Getenv("TERMAIL_CACHE_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserCacheDir() // Windows: %LOCALAPPDATA%
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "Termail"), nil
}

// Store is the set of local files under one base folder. The zero value
// (or nil) does nothing: every load finds nothing, every save is skipped.
type Store struct {
	Dir string
	mu  sync.Mutex
}

// Open returns the store under Base().
func Open() *Store {
	d, err := Base()
	if err != nil {
		return nil
	}
	return &Store{Dir: d}
}

func (s *Store) ok() bool { return s != nil && s.Dir != "" }

func (s *Store) cacheDir() string  { return filepath.Join(s.Dir, "cache") }
func (s *Store) draftsDir() string { return filepath.Join(s.Dir, "taslaklar") }

// writeAtomic writes data next to path and renames it into place, so a
// crash or a killed process never leaves a half-written file behind.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func readSmall(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFile {
		return nil, errors.New("dosya çok büyük")
	}
	return os.ReadFile(path)
}

// ---- inbox cache ----

// Folder is a cached folder entry (name and counts only).
type Folder struct {
	Name   string `json:"n"`
	Role   string `json:"r,omitempty"`
	Delim  rune   `json:"d,omitempty"`
	Total  int    `json:"t,omitempty"`
	Unread int    `json:"u,omitempty"`
}

// Header is one cached list row: headers and flags only.
type Header struct {
	UID      uint32    `json:"i"`
	From     string    `json:"f,omitempty"`
	FromAddr string    `json:"a,omitempty"`
	To       string    `json:"to,omitempty"`
	ToAddr   string    `json:"ta,omitempty"`
	ToPeople string    `json:"tp,omitempty"`
	Subject  string    `json:"s,omitempty"`
	Date     time.Time `json:"d"`
	Seen     bool      `json:"r,omitempty"`
	Flagged  bool      `json:"k,omitempty"`
	Attach   bool      `json:"e,omitempty"`
	ReadAt   time.Time `json:"rt,omitempty"`
}

// Inbox is the cached inbox of one account.
type Inbox struct {
	V             int       `json:"v"`
	Email         string    `json:"email"`
	Saved         time.Time `json:"saved"`
	Folder        string    `json:"folder"`
	Folders       []Folder  `json:"folders,omitempty"`
	List          []Header  `json:"list"`
	UIDValidity   uint32    `json:"uv,omitempty"`
	UIDNext       uint32    `json:"un,omitempty"`
	HighestModSeq uint64    `json:"hm,omitempty"`
}

// MaxCached is how many newest rows are kept per account.
const MaxCached = 20000

func (s *Store) inboxPath(email string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return filepath.Join(s.cacheDir(), "gelen-"+hex.EncodeToString(h[:8])+".json")
}

// LoadInbox returns the cached inbox of an account, or nil when there is
// none or it cannot be trusted (corrupt, other version, other account).
func (s *Store) LoadInbox(email string) *Inbox {
	if !s.ok() {
		return nil
	}
	b, err := readSmall(s.inboxPath(email))
	if err != nil {
		return nil
	}
	b, err = decodeCache(b)
	if err != nil {
		return nil
	}
	var in Inbox
	if json.Unmarshal(b, &in) != nil || (in.V != Version && in.V != VersionLegacy) || !strings.EqualFold(in.Email, email) {
		return nil
	}
	out := in.List[:0]
	for _, h := range in.List {
		if h.UID != 0 && len(out) < MaxCached {
			out = append(out, h)
		}
	}
	in.List = out
	return &in
}

// SaveInbox writes the cache of one account (newest MaxCached rows).
func (s *Store) SaveInbox(in Inbox) error {
	if !s.ok() || in.Email == "" {
		return nil
	}
	in.V = Version
	if len(in.List) > MaxCached {
		in.List = in.List[:MaxCached]
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	gz, err := encodeCache(b)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeAtomic(s.inboxPath(in.Email), gz)
}

func encodeCache(jsonBytes []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(jsonBytes); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeCache accepts gzip v2 files and legacy plain-JSON v1 files.
func decodeCache(b []byte) ([]byte, error) {
	if len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		out, err := io.ReadAll(io.LimitReader(r, maxFile*2))
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	return b, nil
}

// ---- pending server changes ----

// Op is a server change that had not been confirmed when Termail closed.
type Op struct {
	Email  string `json:"email"`
	Kind   string `json:"kind"` // "move" or "flag"
	Folder string `json:"folder"`
	UID    uint32 `json:"uid"`
	Dest   string `json:"dest,omitempty"`
	Flag   string `json:"flag,omitempty"`
	On     bool   `json:"on,omitempty"`
}

type opsFile struct {
	V     int       `json:"v"`
	Saved time.Time `json:"saved"`
	Ops   []Op      `json:"ops"`
}

func (s *Store) opsPath() string { return filepath.Join(s.cacheDir(), "bekleyen-islemler.json") }

// SaveOps stores the pending changes (an empty list removes the file).
func (s *Store) SaveOps(ops []Op) error {
	if !s.ok() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ops) == 0 {
		err := os.Remove(s.opsPath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := json.Marshal(opsFile{V: Version, Saved: time.Now(), Ops: ops})
	if err != nil {
		return err
	}
	return writeAtomic(s.opsPath(), b)
}

// TakeOps returns the stored pending changes and removes the file (they
// live in the program again; it stores what is left when it closes).
func (s *Store) TakeOps() []Op {
	if !s.ok() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := readSmall(s.opsPath())
	if err != nil {
		return nil
	}
	os.Remove(s.opsPath())
	var f opsFile
	if json.Unmarshal(b, &f) != nil || f.V != Version {
		return nil
	}
	var out []Op
	for _, o := range f.Ops {
		if o.Email != "" && o.Folder != "" && o.UID != 0 && (o.Kind == "move" && o.Dest != "" || o.Kind == "flag" && o.Flag != "") {
			out = append(out, o)
		}
	}
	return out
}

// ---- unsent messages ----

// Attachment of a saved message.
type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"type,omitempty"`
	Data        []byte `json:"data"`
}

// Draft is a message that was open (or being sent) when Termail closed.
type Draft struct {
	V       int          `json:"v"`
	Saved   time.Time    `json:"saved"`
	From    string       `json:"from"`
	To      string       `json:"to,omitempty"`
	Cc      string       `json:"cc,omitempty"`
	Subject string       `json:"subject,omitempty"`
	Body    string       `json:"body,omitempty"`
	Sending bool         `json:"sending,omitempty"` // was being sent: may have gone out
	Atts    []Attachment `json:"atts,omitempty"`
}

// SaveDraft stores one unsent message and returns its file.
func (s *Store) SaveDraft(d Draft) (string, error) {
	if !s.ok() {
		return "", nil
	}
	d.V = Version
	if d.Saved.IsZero() {
		d.Saved = time.Now()
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.draftsDir(), 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.draftsDir(), d.Saved.Format("20060102-150405")+"-*.json")
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	return name, writeAtomic(name, b)
}

// TakeDrafts returns the saved unsent messages (oldest first) and removes
// their files; unreadable files are left alone.
func (s *Store) TakeDrafts() []Draft {
	if !s.ok() {
		return nil
	}
	names, _ := filepath.Glob(filepath.Join(s.draftsDir(), "*.json"))
	sort.Strings(names)
	var out []Draft
	for _, n := range names {
		b, err := readSmall(n)
		if err != nil {
			continue
		}
		var d Draft
		if json.Unmarshal(b, &d) != nil || d.V != Version {
			continue
		}
		out = append(out, d)
		os.Remove(n)
	}
	return out
}
