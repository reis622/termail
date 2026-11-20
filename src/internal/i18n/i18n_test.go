package i18n

import (
	"os"
	"testing"
)

func TestTDefaultTurkish(t *testing.T) {
	Set(TR)
	if T("Kaydet") != "Kaydet" || Tf("%d ileti · %d okunmamış", 3, 1) != "3 ileti · 1 okunmamış" {
		t.Fatalf("turkish default: %q %q", T("Kaydet"), Tf("%d ileti · %d okunmamış", 3, 1))
	}
}

func TestTEnglish(t *testing.T) {
	Set(EN)
	t.Cleanup(func() { Set(TR) })
	if T("Kaydet") != "Save" {
		t.Fatalf("Kaydet: %q", T("Kaydet"))
	}
	if T("Gelen Kutusu") != "Inbox" {
		t.Fatalf("Gelen Kutusu: %q", T("Gelen Kutusu"))
	}
	got := Tf("%d ileti · %d okunmamış", 3, 1)
	if got != "3 messages · 1 unread" {
		t.Fatalf("count: %q", got)
	}
}

func TestParseAndResolve(t *testing.T) {
	if l, ok := Parse("en"); !ok || l != EN {
		t.Fatalf("parse en: %q %v", l, ok)
	}
	if l, ok := Parse("Türkçe"); !ok || l != TR {
		t.Fatalf("parse türkçe: %q %v", l, ok)
	}
	if _, ok := Parse(""); ok {
		t.Fatal("empty should not parse")
	}
	if Resolve("en") != EN {
		t.Fatal("resolve en")
	}
}

func TestFilterHints(t *testing.T) {
	Set(TR)
	if FilterHints()[0] != "kimden:" {
		t.Fatalf("tr hints: %v", FilterHints())
	}
	Set(EN)
	t.Cleanup(func() { Set(TR) })
	if FilterHints()[0] != "from:" {
		t.Fatalf("en hints: %v", FilterHints())
	}
}

func TestDetectUsesLANG(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "tr_TR.UTF-8")
	if Detect() != TR {
		t.Fatalf("expected tr, got %q", Detect())
	}
	t.Setenv("LANG", "en_US.UTF-8")
	if Detect() != EN {
		t.Fatalf("expected en, got %q", Detect())
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		os.Unsetenv(k)
	}
	if Detect() != EN {
		t.Fatalf("unset locale should be en, got %q", Detect())
	}
}
