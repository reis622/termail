package mailmsg

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message/mail"
)

func TestBuildAndParseWithAttachments(t *testing.T) {
	pdf := bytes.Repeat([]byte{0x25, 0x50, 0x44, 0x46, 0x00, 0xff}, 5000)
	o := &Outgoing{
		From:      &mail.Address{Name: "Ahmet Yılmaz", Address: "ahmet@gmail.com"},
		To:        []*mail.Address{{Name: "Ayşe Demir", Address: "ayse@firma.com.tr"}},
		Cc:        []*mail.Address{{Address: "mehmet@gmail.com"}},
		Subject:   "Re: Proje toplantısı – yeni saat",
		Body:      "Merhaba Ayşe,\nŞimdi gönderiyorum: çğıöşü ÇĞİÖŞÜ\n",
		InReplyTo: "abc@firma.com.tr", References: []string{"abc@firma.com.tr"},
		Attachments: []Attachment{{Name: "sunum taslağı.pdf", Data: pdf}, {Name: "butce.xlsx", Data: []byte("xlsx")}},
		Date:        time.Date(2026, 11, 14, 14, 36, 0, 0, time.UTC),
	}
	raw, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("multipart/mixed")) {
		t.Fatal("not multipart")
	}
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != o.Subject || p.From[0].Name != "Ahmet Yılmaz" || len(p.Cc) != 1 {
		t.Fatalf("header: %+v", p)
	}
	if !strings.Contains(p.Text, "çğıöşü ÇĞİÖŞÜ") {
		t.Fatalf("text: %q", p.Text)
	}
	if len(p.Attachments) != 2 {
		t.Fatalf("attachments: %d", len(p.Attachments))
	}
	a := p.Attachments[0]
	if a.Name != "sunum taslağı.pdf" || a.ContentType != "application/pdf" || !bytes.Equal(a.Data, pdf) || a.Size != len(pdf) {
		t.Fatalf("att0: %s %s %d", a.Name, a.ContentType, a.Size)
	}
	if len(p.References) != 1 || p.References[0] != "abc@firma.com.tr" {
		t.Fatalf("refs %v", p.References)
	}
	if got := o.Recipients(); len(got) != 2 {
		t.Fatalf("rcpts %v", got)
	}
}

const htmlMail = "From: =?utf-8?q?Turkcell?= <fatura@turkcell.com.tr>\r\n" +
	"To: ahmet@gmail.com\r\nSubject: =?iso-8859-9?q?Kas=FDm_faturas=FD?=\r\n" +
	"Date: Sat, 14 Nov 2026 13:05:00 +0300\r\nMessage-ID: <f1@turkcell.com.tr>\r\n" +
	"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=XX\r\n\r\n" +
	"--XX\r\nContent-Type: multipart/alternative; boundary=YY\r\n\r\n" +
	"--YY\r\nContent-Type: text/html; charset=windows-1254\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"<p>Say=FDn Ahmet,</p><p>Tutar: <b>349,90 TL</b><img src=3D\"https://track.example/p.gif\"></p>\r\n" +
	"--YY--\r\n" +
	"--XX\r\nContent-Type: application/pdf; name=\"fatura.pdf\"\r\nContent-Disposition: attachment; filename=\"fatura.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQ=\r\n" +
	"--XX\r\nContent-Type: image/png\r\nContent-Disposition: inline\r\nContent-Transfer-Encoding: base64\r\n\r\niVBORw0K\r\n" +
	"--XX--\r\n"

func TestParseHTMLOnlyAndCharsets(t *testing.T) {
	p, err := Parse([]byte(htmlMail))
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != "Kasım faturası" {
		t.Fatalf("subject %q", p.Subject)
	}
	if !p.FromHTML || !strings.Contains(p.Text, "Sayın Ahmet,") || strings.Contains(p.Text, "track.example") {
		t.Fatalf("text %q", p.Text)
	}
	if len(p.Attachments) != 2 || p.Attachments[0].Name != "fatura.pdf" || string(p.Attachments[0].Data) != "%PDF-1.4" {
		t.Fatalf("att %+v", p.Attachments)
	}
	if p.Attachments[1].Name != "ek-2.png" {
		t.Fatalf("inline image name %q", p.Attachments[1].Name)
	}
}

func TestPlainSingle(t *testing.T) {
	raw, err := Build(&Outgoing{From: &mail.Address{Address: "a@b.c"}, To: []*mail.Address{{Address: "x@y.z"}}, Subject: "Selam", Body: "Merhaba"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := Parse(raw)
	if p.Text != "Merhaba" || len(p.Attachments) != 0 {
		t.Fatalf("%q %d", p.Text, len(p.Attachments))
	}
}

func TestHelpers(t *testing.T) {
	if ReplySubject("Re: X") != "Re: X" || ReplySubject("X") != "Re: X" || ForwardSubject("X") != "Fwd: X" {
		t.Fatal("subjects")
	}
	l, err := ParseAddresses("Ayşe Demir <ayse@firma.com.tr>;  mehmet.kaya@gmail.com")
	if err != nil || len(l) != 2 || l[0].Name != "Ayşe Demir" {
		t.Fatalf("%v %v", l, err)
	}
	if HumanSize(212*1024) != "212 KB" || HumanSize(1468006) != "1,4 MB" {
		t.Fatal(HumanSize(212*1024), HumanSize(1468006))
	}
	now := time.Date(2026, 11, 14, 15, 0, 0, 0, time.Local)
	if ShortDate(now.Add(-time.Hour), now) != "14:00" || ShortDate(now.AddDate(0, 0, -1), now) != "Dün" || ShortDate(now.AddDate(0, 0, -2), now) != "12 Kas" {
		t.Fatal("dates")
	}
	if LongDate(time.Date(2026, 11, 14, 13, 5, 0, 0, time.Local)) != "14 Kasım 2026, 13:05" {
		t.Fatal(LongDate(now))
	}
}

func TestPlainPlaceholderFallsBackToHTML(t *testing.T) {
	html := "<html><body><h1>KahhveCom Haftalık</h1><p>Bu hafta yeni çekirdeklerimizi denedik. Espresso harmanı ve filtre kahve önerileri içeride.</p><p>Keyifli kahveler dileriz.</p></body></html>"
	raw := "From: KahhveCom <bulten@kahhve.com>\r\n" +
		"To: ahmet@test.local\r\nSubject: Haftalık bulten\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=XX\r\n\r\n" +
		"--XX\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nTEXT_CONTENT\r\n" +
		"--XX\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + html + "\r\n" +
		"--XX--\r\n"
	p, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !p.FromHTML {
		t.Fatal("placeholder plain part should yield to the HTML part")
	}
	if strings.Contains(p.Text, "TEXT_CONTENT") || strings.Contains(p.View, "TEXT_CONTENT") {
		t.Fatalf("reader still shows TEXT_CONTENT: text=%q view=%q", p.Text, p.View)
	}
	if !strings.Contains(p.Text, "KahhveCom") || !strings.Contains(p.Text, "Espresso") {
		t.Fatalf("HTML body not rendered as text: %q", p.Text)
	}

	// whitespace-only plain part
	raw = strings.Replace(raw, "TEXT_CONTENT", "  \n\t  ", 1)
	p, err = Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !p.FromHTML || !strings.Contains(p.Text, "KahhveCom") {
		t.Fatalf("whitespace plain should fall back to HTML: fromHTML=%v text=%q", p.FromHTML, p.Text)
	}

	// a real short plain part that is the whole message must be kept
	short := "From: a@b.com\r\nTo: c@d.com\r\nSubject: x\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=XX\r\n\r\n" +
		"--XX\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nSee you at 5.\r\n" +
		"--XX\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>See you at 5.</p>\r\n" +
		"--XX--\r\n"
	p, err = Parse([]byte(short))
	if err != nil {
		t.Fatal(err)
	}
	if p.FromHTML {
		t.Fatalf("short real text should not be replaced: %q", p.Text)
	}
	if !strings.Contains(p.Text, "See you at 5.") {
		t.Fatalf("plain text lost: %q", p.Text)
	}
}

func TestIsPlainPlaceholder(t *testing.T) {
	for _, s := range []string{"TEXT_CONTENT", " text_content ", "HTML_CONTENT", "BODY_CONTENT"} {
		if !isPlainPlaceholder(s) {
			t.Errorf("%q should be a placeholder", s)
		}
	}
	for _, s := range []string{"See you at 5.", "Merhaba,", "Order 12345 is ready"} {
		if isPlainPlaceholder(s) {
			t.Errorf("%q is real text", s)
		}
	}
}
