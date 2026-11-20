package htmltext

import (
	"strings"
	"testing"
)

func TestBasicParagraphs(t *testing.T) {
	got := Convert(`<html><head><title>x</title><style>p{color:red}</style></head><body><p>Merhaba   Ahmet,</p><p>Faturanız <b>hazır</b>.</p></body></html>`)
	want := "Merhaba Ahmet,\n\nFaturanız hazır."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLinksImagesScripts(t *testing.T) {
	got := Convert(`<div>Detay için <a href="https://ornek.com/f">tıklayın</a><img src="https://iz.example/pixel.gif" width=1><img src="x.png" alt="Logo"><script>alert(1)</script></div>`)
	if !strings.Contains(got, "tıklayın (https://ornek.com/f)") {
		t.Fatalf("link not shown: %q", got)
	}
	if strings.Contains(got, "pixel") || strings.Contains(got, "alert") {
		t.Fatalf("leaked resource: %q", got)
	}
	if !strings.Contains(got, "[Logo]") {
		t.Fatalf("alt missing: %q", got)
	}
}

func TestBrListsEntities(t *testing.T) {
	got := Convert(`Satır 1<br>Satır&nbsp;2 &amp; 3<ul><li>Bir</li><li>İki</li></ul><ol><li>A</li><li>B</li></ol>`)
	for _, want := range []string{"Satır 1\nSatır 2 & 3", "• Bir\n• İki", "1. A\n2. B"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestTableAndHidden(t *testing.T) {
	got := Convert(`<table><tr><td>Tutar</td><td>349,90 TL</td></tr><tr><td>Son ödeme</td><td>28 Kasım</td></tr></table><div style="display: none">gizli</div>`)
	if !strings.Contains(got, "Tutar  349,90 TL") || !strings.Contains(got, "Son ödeme  28 Kasım") {
		t.Fatalf("table: %q", got)
	}
	if strings.Contains(got, "gizli") {
		t.Fatalf("hidden text shown: %q", got)
	}
}

func TestBlockquoteAndPre(t *testing.T) {
	got := Convert(`<p>Cevap</p><blockquote>Eski ileti</blockquote><pre>a  b
c</pre>`)
	if !strings.Contains(got, "> Eski ileti") {
		t.Fatalf("quote: %q", got)
	}
	if !strings.Contains(got, "a  b\nc") {
		t.Fatalf("pre: %q", got)
	}
}

func TestMailtoSameLabel(t *testing.T) {
	got := Convert(`<a href="mailto:a@b.com">a@b.com</a> <a href="https://x.com">https://x.com</a>`)
	if got != "a@b.com https://x.com" {
		t.Fatalf("got %q", got)
	}
}
