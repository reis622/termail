package mailmsg

import (
	"encoding/base64"
	"mime/quotedprintable"
	"strings"
	"testing"
)

const binanceHTML = `<html lang="en" dir="auto" xmlns="http://www.w3.org/1999/xhtml" xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office">
<head><meta http-equiv="Content-Type" content="text/html; charset=UTF-8"><meta name="viewport" content="width=device-width">
<title>Binance TR</title>
<style type="text/css">.warp{width:100%;} body{margin:0}</style>
<!--[if mso]><xml><o:OfficeDocumentSettings><o:PixelsPerInch>96</o:PixelsPerInch></o:OfficeDocumentSettings></xml><![endif]-->
</head>
<body style="margin:0;padding:0;background:#f5f5f5">
<div style="display:none;max-height:0;overflow:hidden">Gizli ön izleme metni</div>
<div class="warp" style="width:100%"><table role="presentation" width="100%"><tr><td>
<h1>Haftal&#305;k B&uuml;lten</h1>
<p>Merhaba, bu hafta Binance TR&apos;de neler oldu?</p>
<p><a href="https://www.binance.tr/kampanya?utm=1">Kampanyaya göz at</a></p>
<!-- yorum -->
<script>alert(1)</script>
</td></tr></table></div></body></html>`

func checkBinance(t *testing.T, name string, raw string) {
	t.Helper()
	p, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for _, bad := range []string{"<html", "<div", "<td", "xmlns", ".warp", "OfficeDocumentSettings", "alert(1)", "yorum", "Gizli ön izleme", "&#305;", "&uuml;"} {
		if strings.Contains(p.Text, bad) || strings.Contains(p.View, bad) {
			t.Errorf("%s: %q left in text:\n%s", name, bad, p.View)
		}
	}
	for _, want := range []string{"Haftalık Bülten", "Binance TR'de neler oldu?", "Kampanyaya göz at"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("%s: missing %q:\n%s", name, want, p.Text)
		}
	}
	if len(p.Links) == 0 || !strings.Contains(p.Links[0].URL, "binance.tr") {
		t.Errorf("%s: link not numbered: %+v", name, p.Links)
	}
	if !p.FromHTML {
		t.Errorf("%s: not treated as HTML", name)
	}
}

func qp(s string) string {
	var b strings.Builder
	w := quotedprintable.NewWriter(&b)
	w.Write([]byte(s))
	w.Close()
	return b.String()
}

func TestHTMLSniffing(t *testing.T) {
	hdr := "From: Binance TR <do-not-reply@binance.tr>\r\nTo: a@b.com\r\nSubject: Bulten\r\nMIME-Version: 1.0\r\n"
	b64 := base64.StdEncoding.EncodeToString([]byte(binanceHTML))
	checkBinance(t, "plain with html", hdr+"Content-Type: text/plain; charset=utf-8\r\n\r\n"+binanceHTML)
	checkBinance(t, "no content-type", hdr+"\r\n"+binanceHTML)
	checkBinance(t, "plain qp", hdr+"Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n"+qp(binanceHTML))
	checkBinance(t, "plain base64", hdr+"Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n"+b64)
	checkBinance(t, "nested multipart, both html", hdr+`Content-Type: multipart/mixed; boundary="o"

--o
Content-Type: multipart/alternative; boundary="i"

--i
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: base64

`+b64+`
--i
Content-Type: text/html; charset=utf-8
Content-Transfer-Encoding: quoted-printable

`+qp(binanceHTML)+`
--i--
--o--
`)
	checkBinance(t, "nested, plain part only html", hdr+`Content-Type: multipart/mixed; boundary="o"

--o
Content-Type: multipart/alternative; boundary="i"

--i
Content-Type: text/plain; charset=utf-8

`+binanceHTML+`
--i--
--o--
`)
}

func TestLooksHTMLKeepsPlainText(t *testing.T) {
	for _, s := range []string{
		"Merhaba,\n<ahmet@ornek.com> adresine yazdım. a < b ve c > d.\nSelam",
		"Fiyat <100 TL> oldu",
		"> alıntı\n> <b> değil",
	} {
		if LooksHTML(s) {
			t.Errorf("plain text taken for HTML: %q", s)
		}
	}
	if !LooksHTML("\ufeff  \r\n<!DOCTYPE html><p>x") || !LooksHTML("<table><tr><td>a</td></tr></table>") {
		t.Error("HTML start not seen")
	}
	if !LooksHTML("Merhaba<br><p>bir</p><p>iki</p><p>üç</p><b>dört</b><br><br>") {
		t.Error("dense tags not seen")
	}
}
