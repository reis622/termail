package downloads

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUniqueName(t *testing.T) {
	taken := map[string]bool{"dosya.pdf": true, "dosya (1).pdf": true, "rapor.tar.gz": true, "README": true}
	ex := func(n string) bool { return taken[n] }
	cases := map[string]string{
		"dosya.pdf":    "dosya (2).pdf",
		"yeni.pdf":     "yeni.pdf",
		"rapor.tar.gz": "rapor.tar (1).gz",
		"README":       "README (1)",
	}
	for in, want := range cases {
		if got := UniqueName(in, ex); got != want {
			t.Errorf("UniqueName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		`..\..\Windows\evil.exe`: "evil.exe",
		"a/b/c.txt":              "c.txt",
		`fat:ura?.pdf`:           "fat_ura_.pdf",
		"CON.txt":                "_CON.txt",
		"  ":                     "ek",
		"..":                     "ek",
		"rapor. ":                "rapor",
		"Kasım faturası.pdf":     "Kasım faturası.pdf",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSaveNumbers(t *testing.T) {
	dir := t.TempDir()
	p1, err := Save(dir, "fatura.pdf", []byte("1"))
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := Save(dir, "fatura.pdf", []byte("2"))
	p3, _ := Save(dir, "fatura.pdf", []byte("3"))
	if filepath.Base(p1) != "fatura.pdf" || filepath.Base(p2) != "fatura (1).pdf" || filepath.Base(p3) != "fatura (2).pdf" {
		t.Fatalf("got %s %s %s", p1, p2, p3)
	}
	b, _ := os.ReadFile(p1)
	if string(b) != "1" {
		t.Fatal("first file overwritten")
	}
}

func TestDirOverride(t *testing.T) {
	d := filepath.Join(t.TempDir(), "dl")
	t.Setenv("TERMAIL_DOWNLOADS", d)
	got, err := Dir()
	if err != nil || got != d {
		t.Fatalf("got %q %v", got, err)
	}
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		t.Fatal("not created")
	}
}
