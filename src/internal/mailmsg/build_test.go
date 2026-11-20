package mailmsg

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message/mail"

	"termail/internal/samplemail"
)

func sampleTurkish(atts ...Attachment) *Outgoing {
	return &Outgoing{
		From:    &mail.Address{Name: "Ahmet Yılmaz", Address: "ahmet.yilmaz@hotmail.com"},
		To:      []*mail.Address{{Name: "Ayşe Demir", Address: "ayse.demir@gmail.com"}},
		Cc:      []*mail.Address{{Address: "ayse.demir@firma.com.tr"}},
		Subject: "bak bura — Çarşamba toplantısı için güncel notlar (İstanbul ofisi, ğüşıöç)",
		Body: "Selam Arthur,\n\nŞu dosyaya bir bak: çok önemli değil ama göz at.\n" +
			".\n" + // a lone dot line (SMTP dot-stuffing is the sender's job)
			"From the start\n" + // mbox-sensitive line
			strings.Repeat("Uzun satır ğüşıöç ", 20) + "\n\nGörüşürüz,\nHüseyin",
		Date:        time.Date(2026, 11, 14, 13, 12, 49, 0, time.FixedZone("+03", 3*3600)),
		Attachments: atts,
	}
}

// strictCheck is a small RFC 5322 / 2045 / 2047 checker for what we send.
func strictCheck(t *testing.T, raw []byte, wantMultipart bool) map[string][]string {
	t.Helper()
	if bytes.Contains(bytes.ReplaceAll(raw, []byte("\r\n"), nil), []byte("\n")) || bytes.Contains(bytes.ReplaceAll(raw, []byte("\r\n"), nil), []byte("\r")) {
		t.Fatal("bare LF or CR in message")
	}
	if !bytes.HasSuffix(raw, []byte("\r\n")) {
		t.Fatal("message does not end with CRLF")
	}
	head, body, ok := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !ok {
		t.Fatal("no header/body separator")
	}
	hdr := map[string][]string{}
	var last string
	for _, l := range strings.Split(string(head), "\r\n") {
		if len(l) > 78 {
			t.Errorf("header line longer than 78: %q", l)
		}
		for _, c := range []byte(l) {
			if c >= 0x80 || (c < 0x20 && c != '\t') {
				t.Fatalf("non-ASCII/control byte in header: %q", l)
			}
		}
		if l != "" && (l[0] == ' ' || l[0] == '\t') {
			hdr[last][len(hdr[last])-1] += " " + strings.TrimSpace(l)
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			t.Fatalf("malformed header line %q", l)
		}
		last = strings.ToLower(k)
		hdr[last] = append(hdr[last], strings.TrimSpace(v))
	}
	for _, k := range []string{"date", "from", "to", "subject", "message-id", "mime-version", "content-type"} {
		if len(hdr[k]) != 1 {
			t.Errorf("header %s appears %d times, want 1", k, len(hdr[k]))
		}
	}
	for k, v := range hdr {
		if len(v) > 1 && k != "received" {
			t.Errorf("duplicate header %s", k)
		}
		if k == "bcc" || strings.HasPrefix(k, "x-") || k == "return-path" || k == "received" || k == "dkim-signature" {
			t.Errorf("header %s must not be in an outgoing message", k)
		}
	}
	if hdr["mime-version"][0] != "1.0" {
		t.Error("MIME-Version")
	}
	if _, err := time.Parse(time.RFC1123Z, hdr["date"][0]); err != nil {
		t.Errorf("Date %q is not RFC 5322 with zone: %v", hdr["date"][0], err)
	}
	if !regexp.MustCompile(`^<[A-Za-z0-9._%+-]+@hotmail\.com>$`).MatchString(hdr["message-id"][0]) {
		t.Errorf("Message-ID %q", hdr["message-id"][0])
	}
	for _, l := range strings.Split(string(body), "\r\n") {
		if len(l) > 998 {
			t.Fatal("body line longer than 998")
		}
		if len(l) > 76 && !strings.HasPrefix(l, "--") {
			t.Errorf("encoded body line longer than 76: %q", l)
		}
		for _, c := range []byte(l) {
			if c >= 0x80 {
				t.Fatalf("8-bit byte in an encoded body: %q", l)
			}
		}
	}
	mt, params, _ := mime.ParseMediaType(hdr["content-type"][0])
	if wantMultipart {
		if mt != "multipart/mixed" || params["boundary"] == "" {
			t.Fatalf("content type %q", hdr["content-type"][0])
		}
		if !bytes.Contains(body, []byte("--"+params["boundary"]+"--")) {
			t.Fatal("multipart not closed")
		}
	} else if mt != "text/plain" || params["charset"] != "utf-8" || hdr["content-transfer-encoding"][0] != "quoted-printable" {
		t.Fatalf("single part headers: %v", hdr)
	}
	return hdr
}

func TestBuildPlainTurkishIsStrict(t *testing.T) {
	o := sampleTurkish()
	raw, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TERMAIL_DUMP") != "" {
		os.WriteFile(os.Getenv("TERMAIL_DUMP"), raw, 0o644)
	}
	hdr := strictCheck(t, raw, false)
	if !strings.HasPrefix(hdr["subject"][0], "=?utf-8?") || !strings.Contains(hdr["from"][0], "=?utf-8?") || !strings.HasSuffix(hdr["from"][0], "<ahmet.yilmaz@hotmail.com>") {
		t.Fatalf("encoded words: subject %q from %q", hdr["subject"][0], hdr["from"][0])
	}
	// Thunderbird sends exactly this kind of header set for a plain mail
	allowed := map[string]bool{"date": true, "from": true, "to": true, "cc": true, "subject": true, "message-id": true, "mime-version": true,
		"content-type": true, "content-transfer-encoding": true, "user-agent": true, "in-reply-to": true, "references": true}
	for k := range hdr {
		if !allowed[k] {
			t.Errorf("unexpected header %s", k)
		}
	}
	// round trip through the parser
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != o.Subject || p.From[0].Name != "Ahmet Yılmaz" || p.From[0].Address != "ahmet.yilmaz@hotmail.com" || len(p.Cc) != 1 {
		t.Fatalf("round trip headers: %+v", p)
	}
	trim := func(x string) string {
		ls := strings.Split(x, "\n")
		for i := range ls {
			ls[i] = strings.TrimRight(ls[i], " ")
		}
		return strings.Join(ls, "\n")
	}
	if p.Text != trim(o.Body) || len(p.Attachments) != 0 {
		t.Fatalf("round trip body:\n%q\n%q", p.Text, o.Body)
	}
}

func TestBuildWithAttachmentsIsStrict(t *testing.T) {
	pdf := bytes.Repeat([]byte("%PDF-1.4 ğ "), 5000)
	o := sampleTurkish(Attachment{Name: "Kasım faturası ğüşİ.pdf", Data: pdf}, Attachment{Name: "notlar.txt", Data: []byte("not\nsatır")})
	raw, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	strictCheck(t, raw, true)
	if bytes.Contains(raw, []byte("Content-Disposition: attachment\r\n")) && !bytes.Contains(raw, []byte("filename")) {
		t.Fatal("attachment without file name")
	}
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	parts := 0
	for {
		pt, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(pt.Body)
		if len(data) == 0 {
			t.Fatal("empty MIME part")
		}
		parts++
		if ah, ok := pt.Header.(*mail.AttachmentHeader); ok {
			name, _ := ah.Filename()
			ct, _, _ := ah.ContentType()
			if name == "Kasım faturası ğüşİ.pdf" && (ct != "application/pdf" || !bytes.Equal(data, pdf)) {
				t.Fatalf("pdf part: %s %d bytes", ct, len(data))
			}
			if name == "notlar.txt" && !strings.HasPrefix(ct, "text/plain") {
				t.Fatalf("txt type %s", ct)
			}
			if ah.Get("Content-Transfer-Encoding") != "base64" {
				t.Fatal("attachment not base64")
			}
		}
	}
	if parts != 3 {
		t.Fatalf("%d parts, want text + 2 attachments", parts)
	}
	// base64 is wrapped at 76
	if i := bytes.Index(raw, []byte("JVBERi0x")); i < 0 || bytes.IndexByte(raw[i:], '\r')-0 > 76 {
		t.Fatal("base64 not wrapped at 76")
	}
	_ = base64.StdEncoding
}

func TestBuildReplyHasThreadingAndNoOriginalHeaders(t *testing.T) {
	var orig []byte
	for _, s := range samplemail.All(time.Now()) {
		if bytes.Contains(s.Raw, []byte("DKIM-Signature")) {
			orig = s.Raw
			break
		}
	}
	if orig == nil {
		t.Skip("no signed sample")
	}
	p, err := Parse(orig)
	if err != nil {
		t.Fatal(err)
	}
	o := sampleTurkish()
	o.Subject = ReplySubject(p.Subject)
	o.Body = "Tamam.\n\n" + QuoteHeader(p) + "\n" + strings.Join(QuoteLines(p.Text), "\n")
	o.InReplyTo = p.MessageID
	o.References = append(append([]string{}, p.References...), p.MessageID)
	raw, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	hdr := strictCheck(t, raw, false)
	if hdr["in-reply-to"][0] != "<"+p.MessageID+">" || !strings.Contains(hdr["references"][0], "<"+p.MessageID+">") {
		t.Fatalf("threading: %q / %q (orig %q)", hdr["in-reply-to"], hdr["references"], p.MessageID)
	}
	rp, _ := Parse(raw)
	for _, bad := range []string{"DKIM-Signature", "Received:", "ARC-Seal", "Content-Transfer-Encoding:", "Return-Path"} {
		if strings.Contains(rp.Text, bad) {
			t.Fatalf("original headers leaked into the reply body (%s)", bad)
		}
	}
}

func TestRiskyAttachments(t *testing.T) {
	exe := append([]byte("MZ"), make([]byte, 100)...)
	cases := map[string]bool{"termail.exe": true, "kur.BAT": true, "x.ps1": true, "rapor.pdf": false, "foto.jpg": false}
	for n, want := range cases {
		if Risky(n, []byte("data")) != want {
			t.Errorf("%s", n)
		}
	}
	if !Risky("program.dat", exe) {
		t.Error("Windows program by content")
	}
	var zb bytes.Buffer
	zw := newZip(&zb, "icinde/termail.exe")
	zw.Close()
	if !Risky("arsiv.zip", zb.Bytes()) {
		t.Error("program inside zip")
	}
}
