package mailmsg

import (
	"strings"
	"testing"
	"time"

	"termail/internal/samplemail"
)

func TestParseRealisticSamples(t *testing.T) {
	for _, s := range samplemail.All(time.Now()) {
		t.Run(s.Key, func(t *testing.T) {
			p, err := Parse(s.Raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !strings.Contains(p.Text, s.WantText) {
				t.Fatalf("body lacks %q:\n%s", s.WantText, p.Text)
			}
			for _, b := range samplemail.Banned {
				if strings.Contains(p.Text, b) {
					t.Fatalf("raw header %q shown in body:\n%s", b, p.Text)
				}
			}
			if p.Subject != s.Subject {
				t.Fatalf("subject %q, want %q", p.Subject, s.Subject)
			}
			if len(p.From) == 0 || p.From[0].Address != s.FromAddr || p.From[0].Name != s.FromName {
				t.Fatalf("from %+v", p.From)
			}
			if s.Attach != (len(p.Attachments) > 0) {
				t.Fatalf("attachments %d", len(p.Attachments))
			}
		})
	}
}

func TestParseAttachmentName(t *testing.T) {
	for _, s := range samplemail.All(time.Now()) {
		if s.Key != "mixed-att" {
			continue
		}
		p, _ := Parse(s.Raw)
		if p.Attachments[0].Name != "ekstre-ekim-2026 özet.pdf" || !strings.HasPrefix(string(p.Attachments[0].Data), "%PDF") {
			t.Fatalf("%q", p.Attachments[0].Name)
		}
	}
}

func TestParseHTMLDropsScriptsAndStyles(t *testing.T) {
	for _, s := range samplemail.All(time.Now()) {
		if s.Key != "html-only" {
			continue
		}
		p, _ := Parse(s.Raw)
		if strings.Contains(p.Text, "var x") || strings.Contains(p.Text, "margin") || strings.Contains(p.Text, "<p>") {
			t.Fatalf("html leaked:\n%s", p.Text)
		}
	}
}

func TestParseGarbageIsNotShownRaw(t *testing.T) {
	_, err := Parse([]byte(" bozuk\r\nbaşlık yok"))
	if err == nil {
		t.Fatal("want error for an unreadable message")
	}
}

func TestPaddingOnlyPlainPartFallsBackToHTML(t *testing.T) {
	pad := strings.Repeat("&#847; &zwnj; &#8199; &#65279; ", 50)
	raw := "From: Kampanya <k@ornek.com>\r\nTo: a@b.com\r\nSubject: =?utf-8?q?F=C4=B1rsat?= \u200c\u2007\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=XX\r\n\r\n" +
		"--XX\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + pad + "\r\n" + pad + "\r\n" +
		"--XX\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<div style=\"display:none\">" + pad + "önizleme</div><p>Asıl metin burada.</p>\r\n--XX--\r\n"
	p, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if p.Text != "Asıl metin burada." {
		t.Fatalf("%q", p.Text)
	}
}
