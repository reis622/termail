package mailmsg

import (
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBrowserHTMLPlainTextIsEscaped(t *testing.T) {
	p := &Parsed{
		From:    []*mail.Address{{Name: "Ayşe", Address: "ayse@x.com"}},
		To:      []*mail.Address{{Address: "ben@test.local"}},
		Subject: "Merhaba <script>",
		Date:    time.Date(2026, 11, 14, 18, 40, 0, 0, time.FixedZone("IST", 3*3600)),
		Text:    "Satır 1\n<a> & ok",
	}
	out := BrowserHTML(p)
	for _, want := range []string{"Kimden", "Ayşe", "Kime", "ben@test.local", "Tarih", "Konu", "Merhaba &lt;script&gt;", "<pre", "Satır 1", "&lt;a&gt; &amp; ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("plain HTML missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "<script>") || strings.Contains(out, "<a>") {
		t.Fatal("unescaped text leaked into the page")
	}
}

func TestBrowserHTMLUsesOriginalHTMLAndInlinesCID(t *testing.T) {
	p := &Parsed{
		From:    []*mail.Address{{Address: "ayse@x.com"}},
		Subject: "Dosyalar",
		HTML:    `<html><body><p>Merhaba</p><img src="cid:logo@x" alt="logo"></body></html>`,
		Attachments: []Attachment{{
			Name:        "logo.png",
			ContentType: "image/png",
			ContentID:   "logo@x",
			Data:        []byte("PNG"),
		}},
	}
	out := BrowserHTML(p)
	if !strings.Contains(out, "Merhaba") || !strings.Contains(out, "Kimden") || !strings.Contains(out, "ayse@x.com") {
		t.Fatalf("header or body missing:\n%s", out)
	}
	if strings.Contains(out, "cid:logo@x") {
		t.Fatal("cid: was not inlined")
	}
	if !strings.Contains(out, `src="data:image/png;base64,`) {
		t.Fatalf("expected data URI, got:\n%s", out)
	}
	// header is injected inside <body>, not wrapped around the document
	if i := strings.Index(out, "<body>"); i < 0 || !strings.Contains(out[i:], "Kimden") || !strings.Contains(out[i:], "<p>Merhaba</p>") {
		t.Fatal("header should be injected inside the existing body")
	}
}
