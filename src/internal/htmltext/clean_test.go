package htmltext

import (
	"strings"
	"testing"
)

// The exact marketing "preheader padding" pattern from a real mail.
const padding = "&#847; &zwnj; &#8199; &#65279; &#847; &zwnj; &#8199; &#65279; &#847; &zwnj; &#8199; &#65279; &#847; &zwnj; &#8199; &#65279; "

func assertClean(t *testing.T, name, got string) {
	t.Helper()
	for _, bad := range []string{"&#", "&zwnj;", "&amp;", "\u034f", "\u200c", "\u2007", "\ufeff", "\u00ad", "\u200b", "\u2060", "\u180e"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%s: %q still contains %q", name, got, bad)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if l != "" && strings.TrimSpace(l) == "" {
			t.Fatalf("%s: whitespace-only line in %q", name, got)
		}
		if strings.Contains(l, "   ") {
			t.Fatalf("%s: space run left in %q", name, l)
		}
	}
}

func TestPreheaderPaddingPlainText(t *testing.T) {
	src := "Kasım fırsatları başladı! " + strings.Repeat(padding, 20) + "\n" + strings.Repeat(padding, 30) + "\n\nMerhaba Ahmet,\n\nYeni ürünler seni bekliyor.\n"
	got := CleanText(src)
	assertClean(t, "plain", got)
	if got != "Kasım fırsatları başladı!\n\nMerhaba Ahmet,\n\nYeni ürünler seni bekliyor." {
		t.Fatalf("%q", got)
	}
}

func TestPreheaderDoubleEscaped(t *testing.T) {
	src := strings.ReplaceAll(padding, "&", "&amp;")
	got := CleanText("Başlık " + strings.Repeat(src, 10) + "\nmetin &amp; AT&T")
	assertClean(t, "double", got)
	if got != "Başlık\nmetin & AT&T" {
		t.Fatalf("%q", got)
	}
}

func TestPreheaderInHTML(t *testing.T) {
	html := `<html><body>
<div style="display:none;font-size:1px;color:#ffffff;line-height:1px;max-height:0px;max-width:0px;opacity:0;overflow:hidden;mso-hide:all;">Gizli önizleme metni ` + strings.Repeat(padding, 40) + `</div>
<span style="mso-hide: all; max-height: 0; overflow: hidden">gizli 2</span>
<div style="max-height:0;overflow:hidden">gizli 3</div>
<p>` + strings.Repeat(padding, 5) + `</p>
<p>Merhaba&nbsp;Ahmet,&#8199;&#847;</p>
<p>` + strings.ReplaceAll(padding, "&", "&amp;") + `İndirim kodun: <b>KASIM25</b></p>
</body></html>`
	got := Convert(html)
	assertClean(t, "html", got)
	if strings.Contains(got, "gizli") || strings.Contains(got, "Gizli") {
		t.Fatalf("hidden preheader shown: %q", got)
	}
	if got != "Merhaba Ahmet,\n\nİndirim kodun: KASIM25" {
		t.Fatalf("%q", got)
	}
}

func TestCleanKeepsFormatting(t *testing.T) {
	src := "Tutar    349,90 TL\n> alıntı\n    kod satırı\n\n\n\nson"
	if got := CleanText(src); got != "Tutar    349,90 TL\n> alıntı\n    kod satırı\n\nson" {
		t.Fatalf("%q", got)
	}
}
