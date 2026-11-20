package htmltext

import (
	"strings"
	"testing"
)

func urls(s *Links) []string {
	var out []string
	for _, l := range s.List {
		out = append(out, l.URL)
	}
	return out
}

func TestLinkifyPlainText(t *testing.T) {
	var set Links
	in := "Merhaba, faturanız https://ornek.com.tr/fatura?id=5. adresinde. " +
		"Ayrıntı için (bkz. https://tr.wikipedia.org/wiki/Ankara_(şehir)) ya da www.örnek.com'a bakın, " +
		"yazın: mailto:destek@ornek.com! Tekrar: https://ornek.com.tr/fatura?id=5"
	out := Linkify(in, &set)
	want := []string{"https://ornek.com.tr/fatura?id=5", "https://tr.wikipedia.org/wiki/Ankara_%28%C5%9Fehir%29", "https://www.xn--rnek-4qa.com", "mailto:destek@ornek.com"}
	if got := urls(&set); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("links\n got %q\nwant %q", got, want)
	}
	if Domain(set.List[2].URL) != "örnek.com" {
		t.Errorf("domain shown as %q", Domain(set.List[2].URL))
	}
	if !strings.Contains(out, "id=5"+Marker(1)+". adresinde") {
		t.Errorf("trailing dot not cut / marker missing: %q", out)
	}
	if !strings.Contains(out, "Ankara_(şehir)"+Marker(2)+")") {
		t.Errorf("parentheses: %q", out)
	}
	if !strings.Contains(out, "www.örnek.com"+Marker(3)+"'a") {
		t.Errorf("Turkish suffix: %q", out)
	}
	if !strings.Contains(out, "destek@ornek.com"+Marker(4)+"!") {
		t.Errorf("mailto: %q", out)
	}
	if strings.Count(out, Marker(1)) != 2 {
		t.Errorf("duplicate did not reuse number 1: %q", out)
	}
}

func TestLinkSchemesFiltered(t *testing.T) {
	var set Links
	for _, u := range []string{"javascript:alert(1)", "data:text/html;base64,AAAA", "cid:img1@x", "file:///C:/Windows/System32/calc.exe",
		"vbscript:x", "#top", "", "http://", "https://user:pw@evil.com/", "ftp://x.com/a"} {
		if n := set.Add(u, "x"); n != 0 {
			t.Errorf("%q accepted as %d", u, n)
		}
	}
	for _, u := range []string{"https://a.com", "HTTP://B.com/x", "mailto:a@b.com", "www.c.com"} {
		if set.Add(u, "") == 0 {
			t.Errorf("%q refused", u)
		}
	}
	if set.Add("https://a.com/", "") != 1 {
		t.Error("trailing slash not deduped")
	}
}

func TestNormalizeIsInjectionSafe(t *testing.T) {
	for _, u := range []string{`https://x.com/"&calc&"`, "https://x.com/a b", "https://x.com/\x00", "https://x.com/\r\nfoo",
		`https://x.com/a\b`, "https://x.com/`id`", "https://x.com/<script>"} {
		if out, ok := Normalize(u); ok && strings.ContainsAny(out, " \"`\\<>\r\n\x00") {
			t.Errorf("%q -> %q still has dangerous characters", u, out)
		}
	}
}

func TestShorten(t *testing.T) {
	long := "https://click.example-shop.com/track/abcdefghijklmnopqrstuvwxyz0123456789/abcdefghijklmnop?utm_source=x&utm_medium=y"
	s := Shorten(long, ShortMax)
	if len([]rune(s)) > ShortMax || !strings.HasPrefix(s, "click.example-shop.com/track/") || !strings.HasSuffix(s, "…") {
		t.Fatalf("short %q", s)
	}
	if Shorten("https://a.com/x", ShortMax) != "https://a.com/x" {
		t.Fatal("short link changed")
	}
	var set Links
	out := Linkify("Tıkla: "+long, &set)
	if strings.Contains(out, "utm_medium") || set.List[0].URL != long {
		t.Fatalf("reader shows long link or lost it: %q / %q", out, set.List[0].URL)
	}
}

func TestConvertLinksHTML(t *testing.T) {
	src := `<p>Merhaba Ayşe,</p>
<p>Siparişini <a href="https://shop.com.tr/siparis/42">buradan takip et</a>. Ayrıca
<a href="https://shop.com.tr/siparis/42/">aynı bağlantı</a> ve <a href="javascript:void(0)">kötü</a>,
<a href="cid:logo">logo</a>, <a href="https://track.x.com/p.gif"><img src="https://track.x.com/p.gif" width="1" height="1"></a>
<a href="https://shop.com.tr/kampanya"><img src="b.png" alt="Kampanya"></a>
Düz adres: https://yardim.shop.com.tr/sss.</p>
<p><a href="https://evil.example/login">https://www.garanti.com.tr</a></p>`
	var set Links
	out := ConvertLinks(src, &set)
	if !strings.Contains(out, "buradan takip et"+Marker(1)+".") {
		t.Errorf("anchor marker: %q", out)
	}
	if !strings.Contains(out, "aynı bağlantı"+Marker(1)) {
		t.Errorf("same link must reuse [1]: %q", out)
	}
	if strings.Contains(out, "javascript") || strings.Contains(out, "cid:") || strings.Contains(out, "track.x.com") {
		t.Errorf("unsafe/tracking link shown: %q", out)
	}
	if !strings.Contains(out, "kötü") || !strings.Contains(out, "logo") {
		t.Errorf("text of refused links must stay: %q", out)
	}
	if strings.Contains(out, "(https://") {
		t.Errorf("addresses in brackets in reader text: %q", out)
	}
	got := urls(&set)
	want := []string{"https://shop.com.tr/siparis/42", "https://shop.com.tr/kampanya", "https://yardim.shop.com.tr/sss", "https://evil.example/login"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("links\n got %q\nwant %q", got, want)
	}
	if set.List[1].Text != "Kampanya" || set.List[0].Text != "buradan takip et" {
		t.Errorf("texts %+v", set.List)
	}
	if !Mismatch(set.List[3].Text, set.List[3].URL) {
		t.Error("phishing link not flagged")
	}
	if strings.Count(out, Marker(4)) != 1 {
		t.Errorf("link text that is an address got two markers: %q", out)
	}
}

func TestMismatch(t *testing.T) {
	cases := []struct {
		text, url string
		want      bool
	}{
		{"https://www.garanti.com.tr", "https://evil.example/login", true},
		{"garanti.com.tr", "https://garanti-giris.xyz/", true},
		{"www.paypal.com/tr", "http://paypa1.co/x", true},
		{"garanti.com.tr", "https://www.garanti.com.tr/kampanya", false},
		{"shop.com", "https://click.shop.com/t/1", false},
		{"Buraya tıklayın", "https://evil.example", false},
		{"fatura.pdf", "https://drive.example.com/f", false},
		{"destek@banka.com", "mailto:destek@banka.com", false},
		{"Hesabım", "https://banka.com", false},
	}
	for _, c := range cases {
		if got := Mismatch(c.text, c.url); got != c.want {
			t.Errorf("Mismatch(%q, %q) = %v", c.text, c.url, got)
		}
	}
}

func TestDomain(t *testing.T) {
	if Domain("https://WWW.Ornek.com.tr/x") != "ornek.com.tr" || Domain("mailto:a@Destek.com?subject=x") != "destek.com" {
		t.Fatal(Domain("https://WWW.Ornek.com.tr/x"), Domain("mailto:a@Destek.com?subject=x"))
	}
}
