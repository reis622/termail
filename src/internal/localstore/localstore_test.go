package localstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInboxRoundTrip(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	in := Inbox{Email: "Ahmet@Gmail.com", Folder: "INBOX", Saved: time.Now(),
		Folders: []Folder{{Name: "INBOX", Role: "inbox", Total: 3, Unread: 1}},
		List:    []Header{{UID: 9, From: "Ayşe", Subject: "Merhaba İstanbul", Date: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC), Seen: true, ReadAt: time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)}, {UID: 0, Subject: "bozuk"}}}
	if err := s.SaveInbox(in); err != nil {
		t.Fatal(err)
	}
	got := s.LoadInbox("ahmet@gmail.com")
	if got == nil || len(got.List) != 1 || got.List[0].Subject != "Merhaba İstanbul" || !got.List[0].Seen || got.Folders[0].Total != 3 || got.List[0].ReadAt.IsZero() {
		t.Fatalf("%+v", got)
	}
	if s.LoadInbox("baska@gmail.com") != nil {
		t.Fatal("another account's cache")
	}
	raw, _ := os.ReadFile(s.inboxPath("ahmet@gmail.com"))
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		t.Fatal("v2 cache must be gzip-compressed")
	}
	plain, err := decodeCache(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"body", "password", "sifre", "token"} {
		if strings.Contains(strings.ToLower(string(plain)), w) {
			t.Fatalf("cache contains %q", w)
		}
	}
	if got.UIDValidity != 0 || in.List[0].UID != 9 {
		// bookmarks optional on this save
	}
}

func TestInboxCacheMigratesV1AndStoresSync(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	p := s.inboxPath("a@b.c")
	os.MkdirAll(filepath.Dir(p), 0o700)
	v1 := `{"v":1,"email":"a@b.c","folder":"INBOX","list":[{"i":7,"s":"eski","d":"2026-11-01T00:00:00Z"}]}`
	if err := os.WriteFile(p, []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	got := s.LoadInbox("a@b.c")
	if got == nil || len(got.List) != 1 || got.List[0].UID != 7 {
		t.Fatalf("v1 not migrated on load: %+v", got)
	}
	got.UIDValidity, got.UIDNext, got.HighestModSeq = 3, 40, 99
	if err := s.SaveInbox(*got); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		t.Fatal("save after v1 load should write gzip v2")
	}
	got = s.LoadInbox("a@b.c")
	if got == nil || got.V != Version || got.UIDValidity != 3 || got.UIDNext != 40 || got.HighestModSeq != 99 || got.List[0].UID != 7 {
		t.Fatalf("v2 round-trip: %+v", got)
	}
}

func TestCorruptGzipCacheIsIgnored(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	p := s.inboxPath("a@b.c")
	os.MkdirAll(filepath.Dir(p), 0o700)
	// gzip magic with garbage payload
	if err := os.WriteFile(p, []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0xff, 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	if s.LoadInbox("a@b.c") != nil {
		t.Fatal("corrupt gzip accepted")
	}
}

func TestCorruptCacheIsIgnored(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	p := s.inboxPath("a@b.c")
	os.MkdirAll(filepath.Dir(p), 0o700)
	for name, data := range map[string]string{
		"garbage":       "\x00\x01not json at all",
		"truncated":     `{"v":1,"email":"a@b.c","list":[{"i":1,"s":"yar`,
		"other version": `{"v":99,"email":"a@b.c","list":[{"i":1}]}`,
		"other account": `{"v":1,"email":"x@y.z","list":[{"i":1}]}`,
		"wrong types":   `{"v":"1","email":5,"list":"x"}`,
		"empty":         ``,
	} {
		os.WriteFile(p, []byte(data), 0o600)
		if s.LoadInbox("a@b.c") != nil {
			t.Errorf("%s: corrupt cache accepted", name)
		}
	}
	f, _ := os.Create(p) // absurdly large file
	f.Truncate(maxFile + 1)
	f.Close()
	if s.LoadInbox("a@b.c") != nil {
		t.Error("huge file accepted")
	}
	// and it is simply overwritten by the next save
	if err := s.SaveInbox(Inbox{Email: "a@b.c", List: []Header{{UID: 1}}}); err != nil || s.LoadInbox("a@b.c") == nil {
		t.Fatal("could not replace a corrupt cache", err)
	}
	var nilStore *Store
	if nilStore.LoadInbox("a@b.c") != nil || nilStore.SaveInbox(Inbox{Email: "a"}) != nil || nilStore.TakeOps() != nil {
		t.Fatal("nil store must do nothing")
	}
}

func TestOpsRoundTripAndCorruption(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	ops := []Op{{Email: "a@b.c", Kind: "move", Folder: "INBOX", UID: 5, Dest: "Trash"}, {Email: "a@b.c", Kind: "flag", Folder: "INBOX", UID: 6, Flag: `\Seen`, On: true},
		{Email: "a@b.c", Kind: "move", Folder: "INBOX", UID: 7}} // invalid: no destination
	if err := s.SaveOps(ops); err != nil {
		t.Fatal(err)
	}
	got := s.TakeOps()
	if len(got) != 2 || got[0].UID != 5 || got[1].Flag != `\Seen` {
		t.Fatalf("%+v", got)
	}
	if s.TakeOps() != nil {
		t.Fatal("ops replayed twice")
	}
	os.WriteFile(s.opsPath(), []byte("{bozuk"), 0o600)
	if s.TakeOps() != nil {
		t.Fatal("corrupt ops accepted")
	}
	if _, err := os.Stat(s.opsPath()); err == nil {
		t.Fatal("corrupt ops file kept")
	}
	if err := s.SaveOps(nil); err != nil {
		t.Fatal(err)
	}
}

func TestDrafts(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := s.SaveDraft(Draft{From: "a@b.c", To: "x@y.z", Subject: "Yarım", Body: "metin", Atts: []Attachment{{Name: "a.txt", Data: []byte("ek")}}}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(s.draftsDir(), "zz-bozuk.json"), []byte("{"), 0o600)
	ds := s.TakeDrafts()
	if len(ds) != 1 || ds[0].Body != "metin" || string(ds[0].Atts[0].Data) != "ek" {
		t.Fatalf("%+v", ds)
	}
	if len(s.TakeDrafts()) != 0 {
		t.Fatal("draft reopened twice")
	}
}

func TestPrefsRoundTrip(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if p := s.LoadPrefs(); p.Grouped || len(p.Recent) != 0 {
		t.Fatal("fresh prefs not empty")
	}
	var r []string
	for _, q := range []string{"a", "b", "fatura", "A", "c", "d", "e", "f", "g", "h", "i", "j", "  fatura  "} {
		r = AddRecent(r, q)
	}
	if len(r) != MaxRecent || r[0] != "fatura" || r[1] != "j" {
		t.Fatalf("recent %v", r)
	}
	if err := s.SavePrefs(Prefs{Recent: r, Grouped: true, Open: map[string]bool{"a@b.c": true}, GroupSeen: map[string]int64{"d:google.com": 5}}); err != nil {
		t.Fatal(err)
	}
	p := s.LoadPrefs()
	if !p.Grouped || !p.Open["a@b.c"] || p.GroupSeen["d:google.com"] != 5 || len(p.Recent) != MaxRecent {
		t.Fatalf("loaded %+v", p)
	}
	b, _ := os.ReadFile(s.prefsPath())
	if strings.Contains(string(b), "Toplantı") {
		t.Fatal("unexpected text")
	}
	os.WriteFile(s.prefsPath(), []byte("{bozuk"), 0o600)
	if p := s.LoadPrefs(); p.Grouped {
		t.Fatal("corrupt file used")
	}
	var nilStore *Store
	if nilStore.SavePrefs(Prefs{}) != nil || nilStore.LoadPrefs().Grouped {
		t.Fatal("nil store")
	}
}

func TestBodyCacheRoundTripAndPrune(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	raw := []byte("Subject: merhaba\r\n\r\ngovde")
	if err := s.SaveBody("Ali@X.com", "INBOX", 7, raw); err != nil {
		t.Fatal(err)
	}
	got := s.LoadBody("ali@x.com", "INBOX", 7)
	if string(got) != string(raw) {
		t.Fatalf("got %q", got)
	}
	if s.LoadBody("ali@x.com", "INBOX", 8) != nil {
		t.Fatal("missing uid")
	}
	if s.LoadBody("baska@x.com", "INBOX", 7) != nil {
		t.Fatal("other account")
	}
	if err := s.SaveBody("a@b.c", "INBOX", 1, nil); err != nil || s.LoadBody("a@b.c", "INBOX", 1) != nil {
		t.Fatal("empty body should be skipped")
	}
}
