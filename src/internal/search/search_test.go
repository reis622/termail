package search

import (
	"reflect"
	"testing"
	"time"
)

func TestFoldTurkish(t *testing.T) {
	cases := map[string]string{
		"AYŞE":          "ayse",
		"Ayşe":          "ayse",
		"İSTANBUL":      "istanbul",
		"ISPARTA":       "isparta",
		"ılık":          "ilik",
		"ÇĞÖÜ":          "cgou",
		"Fatura":        "fatura",
		"Kasım":         "kasim",
		"Müşteri":       "musteri",
		"café":          "cafe",
		"明日":            "明日",
		"plain ascii 1": "plain ascii 1",
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
		if len([]rune(Fold(in))) != len([]rune(in)) {
			t.Errorf("Fold(%q) changed the rune count", in)
		}
	}
}

func TestParseFilters(t *testing.T) {
	q := Parse("kimden:ayşe ekli bu hafta proje")
	if !reflect.DeepEqual(q.From, []string{"ayşe"}) || !q.Attach || q.Date != DateThisWeek || !reflect.DeepEqual(q.Words, []string{"proje"}) {
		t.Fatalf("parse: %+v", q)
	}
	if got := q.Chips(); !reflect.DeepEqual(got, []string{"Kimden: Ayşe", "Ekli", "Bu hafta"}) {
		t.Fatalf("chips: %v", got)
	}
	q = Parse("KİMDEN: Mehmet OKUNMAMIŞ Kaydedilen bugün")
	if !reflect.DeepEqual(q.From, []string{"Mehmet"}) || !q.Unread || !q.Saved || q.Date != DateToday || len(q.Words) != 0 {
		t.Fatalf("parse 2: %+v", q)
	}
	q = Parse(`"toplantı notları" bu ay dün`)
	if !reflect.DeepEqual(q.Words, []string{"toplantı notları"}) || q.Date != DateYesterday {
		t.Fatalf("parse 3: %+v", q)
	}
	q = Parse("bu akşam") // "bu" is only a filter before hafta / ay
	if !reflect.DeepEqual(q.Words, []string{"bu", "akşam"}) || q.HasFilters() {
		t.Fatalf("parse 4: %+v", q)
	}
	if !Parse("   ").Empty() || Parse("ekli").Empty() {
		t.Fatal("Empty")
	}
	q = Parse("kimden: ekli") // a filter word is not taken as the sender
	if len(q.From) != 0 || !q.Attach {
		t.Fatalf("parse 5: %+v", q)
	}
}

func TestMatch(t *testing.T) {
	now := time.Date(2026, 11, 14, 15, 0, 0, 0, time.Local) // Saturday
	it := Item{FromHay: Fold("Ayşe Demir ayse.demir@firma.com"), Hay: Fold("Ayşe Demir ayse.demir@firma.com Proje toplantısı"),
		Body: Fold("Toplantıyı perşembe 15:00'e aldık."), Date: now.Add(-2 * time.Hour), Attach: true}
	for q, want := range map[string]bool{
		"ayse":                  true,
		"AYŞE toplantı":         true,
		"TOPLANTIYI":            true,
		"persembe":              true,
		"kimden:ayşe":           true,
		"kimden:mehmet":         false,
		"ekli bugün":            true,
		"dün":                   false,
		"bu hafta":              true,
		"bu ay":                 true,
		"okunmamış":             true,
		"kaydedilen":            false,
		"proje olmayankelime":   false,
		"kimden:firma.com ekli": true,
	} {
		if got := Parse(q).Match(it, now); got != want {
			t.Errorf("%q: got %v want %v", q, got, want)
		}
	}
	it.Seen = true
	if Parse("okunmamış").Match(it, now) {
		t.Fatal("seen mail matched okunmamış")
	}
	// week starts on Monday: Monday 9 Nov is in, Sunday 8 Nov is out
	since, _ := Parse("bu hafta").Range(now)
	if since.Day() != 9 {
		t.Fatalf("week starts %v", since)
	}
	it.Date = time.Date(2026, 11, 13, 23, 0, 0, 0, time.Local)
	if !Parse("dün").Match(it, now) || Parse("bugün").Match(it, now) {
		t.Fatal("yesterday")
	}
}

func TestSpans(t *testing.T) {
	got := Spans("Kasım 2026 FATURANIZ hazır; faturanız", []string{"fatura"})
	if !reflect.DeepEqual(got, [][2]int{{11, 17}, {28, 34}}) {
		t.Fatalf("spans %v", got)
	}
	if got := Spans("Ayşe Demir", []string{"ayse"}); !reflect.DeepEqual(got, [][2]int{{0, 4}}) {
		t.Fatalf("turkish spans %v", got)
	}
	if got := Spans("İstanbul", []string{"ist"}); !reflect.DeepEqual(got, [][2]int{{0, 3}}) {
		t.Fatalf("İ spans %v", got)
	}
	if Spans("abc", nil) != nil || Spans("abc", []string{"x"}) != nil {
		t.Fatal("no match")
	}
}

func TestSnippet(t *testing.T) {
	s := Snippet("Sayın Ahmet Yılmaz,\n\n0532 *** ** 47 numaralı hattınıza ait Kasım 2026\nfaturanız oluşturulmuştur.", []string{"fatura"}, 40)
	if s == "" || []rune(s)[0] != '…' || len([]rune(s)) > 41 {
		t.Fatalf("snippet %q", s)
	}
	if Snippet("hiç yok", []string{"fatura"}, 40) != "" {
		t.Fatal("no match snippet")
	}
}

func TestGrouping(t *testing.T) {
	for addr, want := range map[string]string{
		"no-reply@accounts.google.com": "d:google.com",
		"bildirim@google.com":          "d:google.com",
		"fatura@turkcell.com.tr":       "d:turkcell.com.tr",
		"x@mail.bbc.co.uk":             "d:bbc.co.uk",
		"oidb@univ.edu.tr":             "d:univ.edu.tr",
		"mehmet.kaya@gmail.com":        "a:mehmet.kaya@gmail.com",
		"Ali@Hotmail.com.tr":           "a:ali@hotmail.com.tr",
		"bozuk-adres":                  "a:bozuk-adres",
	} {
		if got := GroupKey(addr); got != want {
			t.Errorf("GroupKey(%q) = %q, want %q", addr, got, want)
		}
	}
	if got := GroupLabel("d:google.com", []string{"Google Play", "Google", "google"}); got != "Google Play" && got != "Google" {
		t.Fatalf("label %q", got)
	}
	if got := GroupLabel("d:google.com", []string{"Google Play", "Google"}); got != "Google" {
		t.Fatalf("label %q", got)
	}
	if got := GroupLabel("d:trendyol.com", []string{"Trendyol", "Trendyol Kampanya"}); got != "Trendyol" {
		t.Fatalf("label %q", got)
	}
	if got := GroupLabel("d:igdas.com.tr", []string{"Fatura", "Müşteri Hizmetleri"}); got != "İgdas" {
		t.Fatalf("label %q", got)
	}
	if got := GroupLabel("d:istanbul.gov.tr", []string{"A", "B"}); got != "İstanbul" {
		t.Fatalf("label %q", got)
	}
}
