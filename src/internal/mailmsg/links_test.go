package mailmsg

import (
	"strings"
	"testing"

	"termail/internal/htmltext"
)

func TestParseCollectsLinks(t *testing.T) {
	html := "From: a@b.com\r\nTo: c@d.com\r\nSubject: x\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<p>Siparişin <a href="https://shop.com.tr/s/1">burada</a>. Yardım: https://shop.com.tr/yardim</p>`
	p, err := Parse([]byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Links) != 2 || p.Links[0].URL != "https://shop.com.tr/s/1" || p.Links[1].URL != "https://shop.com.tr/yardim" {
		t.Fatalf("links %+v", p.Links)
	}
	if !strings.Contains(p.View, "burada"+htmltext.Marker(1)) || !strings.Contains(p.View, htmltext.Marker(2)) {
		t.Fatalf("view %q", p.View)
	}
	// quoting / copying keeps using the plain text with the address written out
	if strings.ContainsRune(p.Text, htmltext.MarkRune) || !strings.Contains(p.Text, "(https://shop.com.tr/s/1)") {
		t.Fatalf("text %q", p.Text)
	}

	plain := "From: a@b.com\r\nTo: c@d.com\r\nSubject: x\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nBak: https://a.com/x, sonra \ue000 www.b.com.\r\n"
	p, err = Parse([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Links) != 2 || strings.Count(p.View, string(htmltext.MarkRune)) != 2 {
		t.Fatalf("plain: %+v %q", p.Links, p.View)
	}
}
