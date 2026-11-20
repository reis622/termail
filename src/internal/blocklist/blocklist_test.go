package blocklist

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMatching(t *testing.T) {
	l, _ := Load("")
	now := time.Now()
	l.Add("ahmet@gmail.com", Address, "Kampanya@Firsat.com", now)
	l.Add("ahmet@gmail.com", Domain, "@Reklam.com", now)
	cases := []struct {
		acct, from string
		want       bool
	}{
		{"ahmet@gmail.com", "kampanya@firsat.com", true},
		{"AHMET@gmail.com", "KAMPANYA@FIRSAT.COM", true},
		{"ahmet@gmail.com", "baska@firsat.com", false}, // address rule only
		{"ahmet@gmail.com", "x@reklam.com", true},      // exact domain
		{"ahmet@gmail.com", "x@mail.REKLAM.com", true}, // subdomain
		{"ahmet@gmail.com", "x@iyireklam.com", false},  // not a subdomain
		{"ahmet@gmail.com", "x@reklam.com.tr", false},
		{"is@firma.com.tr", "x@reklam.com", false}, // other account
		{"ahmet@gmail.com", "", false},
	}
	for _, c := range cases {
		if _, got := l.Match(c.acct, c.from); got != c.want {
			t.Errorf("Match(%q,%q)=%v want %v", c.acct, c.from, got, c.want)
		}
	}
	if _, added := l.Add("ahmet@gmail.com", Domain, "reklam.com", now); added {
		t.Error("duplicate added")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Termail", "blocklist.json")
	l, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	l.Add("a@x.com", Address, "spam@s.com", t0)
	l.Add("a@x.com", Domain, "reklam.com", t0.Add(time.Hour))
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	l2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	all := l2.All()
	if len(all) != 2 || all[0].Value != "reklam.com" || all[0].Kind != Domain || !all[1].Added.Equal(t0) {
		t.Fatalf("%+v", all)
	}
	if !l2.Remove("A@X.com", Domain, "@reklam.com") || l2.Remove("a@x.com", Domain, "reklam.com") {
		t.Fatal("remove")
	}
	l2.Save()
	l3, _ := Load(p)
	if len(l3.All()) != 1 {
		t.Fatal("removal not persisted")
	}
	if all[0].Display() != "@reklam.com" || all[0].Label() != "Alan adı" {
		t.Fatal("labels")
	}
}

func TestBaseDomain(t *testing.T) {
	for in, want := range map[string]string{
		"kampanya@bulten.reklam.com": "reklam.com",
		"x@a.firma.com.tr":           "firma.com.tr",
		"Ali@Gmail.com":              "gmail.com",
	} {
		if got := BaseDomain(in); got != want {
			t.Errorf("BaseDomain(%q)=%q want %q", in, got, want)
		}
	}
}
