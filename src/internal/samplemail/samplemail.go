// Package samplemail holds realistic raw messages as real providers deliver
// them (Gmail-style signed headers, nested multiparts, legacy Turkish
// charsets, encoded subjects with emoji, delivery reports). They are used by
// the parser tests, the demo mode and the local test server.
package samplemail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// Sample is one raw message plus what the list should show for it.
type Sample struct {
	Key      string
	FromName string
	FromAddr string
	Subject  string // decoded
	Date     time.Time
	Raw      []byte
	Attach   bool
	// WantText is a phrase the parsed body must contain; NotText must not
	// appear (e.g. raw header names).
	WantText string
}

// Banned is text that must never show up in a displayed body.
var Banned = []string{"DKIM-Signature", "ARC-Authentication-Results", "X-Google-DKIM-Signature", "ARC-Seal", "h=to:subject", "Content-Transfer-Encoding", "Received: by"}

const gmailTrace = `Delivered-To: ahmet.yilmaz@gmail.com
Received: by 2002:a05:6a10:8a4e:b0:5c9:1f2e:9d3a with SMTP id l14csp1834521pxu;
        Sat, 14 Nov 2026 01:15:08 -0800 (PST)
X-Received: by 2002:a17:90b:4b8f:b0:2ff:6a0b:1c9e with SMTP id lr15mr6032115pjb.12.1763111708123;
        Sat, 14 Nov 2026 01:15:08 -0800 (PST)
ARC-Seal: i=1; a=rsa-sha256; t=1763111708; cv=none;
        d=google.com; s=arc-20240605;
        b=Qm9vT2pX1sF0b7r2Yw8w1QqkUu0gk3m3cJm3Y8m0m3A1QpW9sWb2c1v5t7Y2k0Rk
         9f3d1o2Lw8sQm1VbX6pZ2cR0fT9gH4jK7lM3nP5qS8uV1wX4yZ7aB0cD3eF6gH9iJ2
         kL5mN8oP1qR4sT7uV0wX3yZ6aB9cD2eF5gH8iJ1kL4mN7oP0qR3sT6uV9wX2yZ5aB8==
ARC-Message-Signature: i=1; a=rsa-sha256; c=relaxed/relaxed; d=google.com; s=arc-20240605;
        h=to:subject:message-id:date:from:mime-version:dkim-signature;
        bh=47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=;
        fh=Zm9vYmFyYmF6cXV4; b=c2lnbmF0dXJlZGF0YWdvZXNoZXJlYW5kaXRpc3ZlcnlsbG9uZw
         ZGF0YWdvZXNoZXJlYW5kaXRpc3ZlcnlsbG9uZ2RhdGFnb2VzaGVyZWFuZGl0aXN2ZXJ5
ARC-Authentication-Results: i=1; mx.google.com;
       dkim=pass header.i=@accounts.google.com header.s=20230601 header.b=AbCdEf12;
       spf=pass (google.com: domain of 3hdk2zwgtcbktu-xkvreg-h7pqlh.dkk@gaia.bounces.google.com designates 209.85.220.73 as permitted sender) smtp.mailfrom=3HDk2ZwgTCbkTU-XkVReg-H7PQLh.dKK@gaia.bounces.google.com;
       dmarc=pass (p=REJECT sp=REJECT dis=NONE) header.from=accounts.google.com
Return-Path: <3HDk2ZwgTCbkTU-XkVReg-H7PQLh.dKK@gaia.bounces.google.com>
Received: from mail-sor-f73.google.com (mail-sor-f73.google.com. [209.85.220.73])
        by mx.google.com with SMTPS id d9443c01a7336-2a0b3c4d5e6sor1234567ad.4.2026.11.14.01.15.07
        for <ahmet.yilmaz@gmail.com>
        (Google Transport Security);
        Sat, 14 Nov 2026 01:15:07 -0800 (PST)
Received-SPF: pass (google.com: domain of 3hdk2zwgtcbktu-xkvreg-h7pqlh.dkk@gaia.bounces.google.com designates 209.85.220.73 as permitted sender) client-ip=209.85.220.73;
Authentication-Results: mx.google.com;
       dkim=pass header.i=@accounts.google.com header.s=20230601 header.b=AbCdEf12;
       spf=pass smtp.mailfrom=3HDk2ZwgTCbkTU-XkVReg-H7PQLh.dKK@gaia.bounces.google.com;
       dmarc=pass (p=REJECT sp=REJECT dis=NONE) header.from=accounts.google.com
DKIM-Signature: v=1; a=rsa-sha256; c=relaxed/relaxed;
        d=accounts.google.com; s=20230601; t=1763111707; x=1763716507; dara=google.com;
        h=to:subject:message-id:date:from:mime-version:from:to:cc:subject
         :date:message-id:reply-to;
        bh=47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=;
        b=bh5zk65ki0L6uvSlbkXVGeSRORdpOI1s+u8I/1bvpMN8+aGthWQbEh4ZqvP3N9wQ
         tEBz4K+vLytpvxmAuqdzKerg4JxpW1PiqVvxJ/QY94whPmEJR8oX9iqetLt/cz7mDQu
         twANh5I2abj+kAWjOtgC7X6ZcPdOcX9Ruktf0Yr+76Vf1zNRkr8DQ8KeZwfygyXbeL==
X-Google-DKIM-Signature: v=1; a=rsa-sha256; c=relaxed/relaxed;
        d=1e100.net; s=20251104; t=1763111707; x=1763716507;
        h=to:subject:message-id:date:from:mime-version:x-gm-message-state
         :from:to:cc:subject:date:message-id:reply-to;
        bh=47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=;
        b=UGgrr3qv1xQ8f2bP0mXkS9tLc4rN7yE1uH6jW3aZ5dV8oI2pK4sR7tY0wQ3eT6uI9oP
X-Gm-Message-State: AOJu0YzK8w3n5r7t9v1x3z5b7d9f1h3j5l7n9p1r3t5v7x9z1b3d5f7h
        9j1l3n5p7r9t1v3x5z7b9d1f3h5j7l9n1p3r5t7v9x1z3b5d7f9h1j3l5n7p9r1t3v5x
X-Google-Smtp-Source: AGHT+IFq1w3e5r7t9y1u3i5o7p9a1s3d5f7g9h1j3k5l7z9x1c3v5b7n9m==
`

func crlf(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

func b64lines(b []byte) string {
	enc := base64.StdEncoding.EncodeToString(b)
	var sb strings.Builder
	for len(enc) > 76 {
		sb.WriteString(enc[:76] + "\n")
		enc = enc[76:]
	}
	sb.WriteString(enc + "\n")
	return sb.String()
}

func qp(b []byte) string {
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	w.Write(b)
	w.Close()
	return buf.String()
}

func subj(s string) string { return mime.BEncoding.Encode("UTF-8", s) }

func date(t time.Time) string { return t.Format(time.RFC1123Z) }

// All returns the samples dated relative to now (newest first).
func All(now time.Time) []Sample {
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour).Truncate(time.Minute) }
	var out []Sample
	add := func(s Sample, raw string) {
		s.Raw = []byte(crlf(raw))
		out = append(out, s)
	}

	// 1. Gmail security alert: heavy signed headers, alternative text + html.
	d := ago(1)
	add(Sample{Key: "gmail-alt", FromName: "Google", FromAddr: "no-reply@accounts.google.com", Subject: "Güvenlik uyarısı", Date: d,
		WantText: "Yeni bir cihazdan giriş yapıldı"}, gmailTrace+`MIME-Version: 1.0
Date: `+date(d)+`
Message-ID: <a1b2c3d4e5f6.0.1763111707@accounts.google.com>
Subject: `+subj("Güvenlik uyarısı")+`
From: Google <no-reply@accounts.google.com>
To: ahmet.yilmaz@gmail.com
Content-Type: multipart/alternative; boundary="000000000000a1b2c3061f4e5d6c"

--000000000000a1b2c3061f4e5d6c
Content-Type: text/plain; charset="UTF-8"; format=flowed; delsp=yes
Content-Transfer-Encoding: base64

`+b64lines([]byte("Yeni bir cihazdan giriş yapıldı\r\n\r\nGoogle Hesabınıza Windows üzerinde yeni bir cihazdan giriş yapıldı.\r\nBu siz değilseniz hesabınızı hemen kontrol edin.\r\n\r\nEtkinliği kontrol et: https://accounts.google.com/AccountChooser\r\n"))+`
--000000000000a1b2c3061f4e5d6c
Content-Type: text/html; charset="UTF-8"
Content-Transfer-Encoding: quoted-printable

`+qp([]byte(`<!DOCTYPE html><html lang="tr"><head><style>.a{color:#1a73e8}</style></head><body><table width="100%"><tr><td><div style="font-family:Roboto">Yeni bir cihazdan giriş yapıldı</div><p>Google Hesabınıza yeni bir cihazdan giriş yapıldı.</p><a class="a" href="https://accounts.google.com">Etkinliği kontrol et</a></td></tr></table></body></html>`))+`
--000000000000a1b2c3061f4e5d6c--
`)

	// 2. HTML only, long Turkish subject (the one that overflowed the list).
	d = ago(3)
	long := "Google Play'de satın alma işlemlerini doğrulama ayarlarındaki değişiklikler hakkında bilgilendirme"
	add(Sample{Key: "html-only", FromName: "Google Play", FromAddr: "googleplay-noreply@google.com", Subject: long, Date: d,
		WantText: "satın alma onayı"}, gmailTrace+`MIME-Version: 1.0
Date: `+date(d)+`
Message-ID: <play-77a1@google.com>
Subject: `+subj(long)+`
From: Google Play <googleplay-noreply@google.com>
To: ahmet.yilmaz@gmail.com
Content-Type: text/html; charset="UTF-8"
Content-Transfer-Encoding: quoted-printable

`+qp([]byte(`<html><head><title>Google Play</title><style>p{margin:0}</style><script>var x=1;</script></head><body>
<h1>Ayarlarınızda değişiklik</h1>
<p>Merhaba Ahmet,</p>
<p>Google Play'de satın alma onayı artık her satın almada istenecek. Bu ayarı istediğiniz zaman değiştirebilirsiniz.</p>
<ul><li>Aile kitaplığı</li><li>Ödeme yöntemleri</li></ul>
<p>Teşekkürler,<br>Google Play Ekibi</p></body></html>`))+`
`)

	// 3. Quoted-printable, ISO-8859-9 (Turkish) plain text.
	d = ago(6)
	t, _ := charmap.ISO8859_9.NewEncoder().String("Sayın Ahmet Yılmaz,\r\n\r\nŞubat ayı aidat ödemeniz alınmıştır. Teşekkür ederiz.\r\nİyi günler dileriz.\r\nÇankaya Sitesi Yönetimi\r\n")
	add(Sample{Key: "qp-latin5", FromName: "Site Yönetimi", FromAddr: "yonetim@cankayasitesi.com.tr", Subject: "Aidat ödemeniz alındı", Date: d,
		WantText: "Şubat ayı aidat ödemeniz alınmıştır"}, `Return-Path: <yonetim@cankayasitesi.com.tr>
Received: from mx.cankayasitesi.com.tr (mx.cankayasitesi.com.tr [85.111.1.2])
        by mx.google.com with ESMTPS id x1si123456wrq.2026.11.14
        for <ahmet.yilmaz@gmail.com>;
        Sat, 14 Nov 2026 03:10:00 -0800 (PST)
MIME-Version: 1.0
Date: `+date(d)+`
Message-ID: <aidat-2026@cankayasitesi.com.tr>
Subject: =?ISO-8859-9?Q?Aidat_=F6demeniz_al=FDnd=FD?=
From: =?ISO-8859-9?Q?Site_Y=F6netimi?= <yonetim@cankayasitesi.com.tr>
To: Ahmet <ahmet.yilmaz@gmail.com>
Content-Type: text/plain; charset=ISO-8859-9
Content-Transfer-Encoding: quoted-printable

`+qp([]byte(t))+`
`)

	// 4. Base64, windows-1254 plain text.
	d = ago(20)
	t, _ = charmap.Windows1254.NewEncoder().String("Değerli müşterimiz,\r\n\r\nKasım ayı faturanız 349,90 TL tutarındadır. Son ödeme tarihi 25 Kasım.\r\nÖdemenizi şubelerimizden ya da uygulamadan yapabilirsiniz.\r\n")
	add(Sample{Key: "b64-1254", FromName: "İstanbul İl Ambalaj Atık Yönetimi", FromAddr: "bilgi@istanbul-ambalaj.org.tr", Subject: "Ynt: Kasım ayı faturanız", Date: d,
		WantText: "Kasım ayı faturanız 349,90 TL"}, `Received: from smtp.istanbul-ambalaj.org.tr ([212.156.1.1])
        by mx.google.com with ESMTP id z3si1;
        Fri, 13 Nov 2026 14:00:00 +0300
MIME-Version: 1.0
Date: `+date(d)+`
Subject: `+enc1254("Ynt: Kasım ayı faturanız")+`
From: =?windows-1254?Q?=DDstanbul_=DDl_Ambalaj_At=FDk_Y=F6netimi?= <bilgi@istanbul-ambalaj.org.tr>
To: ahmet.yilmaz@gmail.com
Content-Type: text/plain; charset="windows-1254"
Content-Transfer-Encoding: base64

`+b64lines([]byte(t))+`
`)

	// 5. multipart/mixed > multipart/alternative + PDF attachment.
	d = ago(26)
	add(Sample{Key: "mixed-att", FromName: "Muhasebe", FromAddr: "muhasebe@ornekfirma.com.tr", Subject: "Ekim ayı ekstresi (PDF ekte)", Date: d, Attach: true,
		WantText: "Ekim ayı ekstrenizi ekte bulabilirsiniz"}, gmailTrace+`MIME-Version: 1.0
Date: `+date(d)+`
Subject: `+subj("Ekim ayı ekstresi (PDF ekte)")+`
From: Muhasebe <muhasebe@ornekfirma.com.tr>
To: ahmet.yilmaz@gmail.com
Content-Type: multipart/mixed; boundary="mix-1"

This is a multi-part message in MIME format.

--mix-1
Content-Type: multipart/alternative; boundary="alt-1"

--alt-1
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: 8bit

Merhaba,

Ekim ayı ekstrenizi ekte bulabilirsiniz.

İyi çalışmalar.
--alt-1
Content-Type: text/html; charset=utf-8
Content-Transfer-Encoding: 8bit

<p>Merhaba,</p><p>Ekim ayı ekstrenizi ekte bulabilirsiniz.</p>
--alt-1--

--mix-1
Content-Type: application/pdf; name="=?UTF-8?Q?ekstre-ekim-2026_=C3=B6zet.pdf?="
Content-Disposition: attachment; filename*=UTF-8''ekstre-ekim-2026%20%C3%B6zet.pdf
Content-Transfer-Encoding: base64

`+b64lines([]byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n"))+`
--mix-1--
`)

	// 6. Emoji in subject and sender name (RFC 2047), checkbox emoji with
	// variation selector, CJK, tabs and a stray carriage return.
	d = ago(30)
	es := "☑️ Ali, finish setting up your Windows with Google 🚀"
	add(Sample{Key: "emoji", FromName: "Google 🎉", FromAddr: "no-reply@google.com", Subject: es, Date: d,
		WantText: "Kurulumu tamamla"}, gmailTrace+`MIME-Version: 1.0
Date: `+date(d)+`
Subject: `+subj(es)+`
From: `+subj("Google 🎉")+` <no-reply@google.com>
To: ahmet.yilmaz@gmail.com
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: quoted-printable

`+qp([]byte("Kurulumu tamamla 👉 3 adım kaldı ✅\r\n\tSekme ile girintili satır\r\n東京オフィス: 会議は明日です。\r\n"))+`
`)
	d = ago(31)
	cjk := "会議のお知らせ\t– 東京オフィス\rİstanbul ekibi"
	add(Sample{Key: "cjk", FromName: "山田 太郎", FromAddr: "yamada@example.jp", Subject: cjk, Date: d,
		WantText: "明日"}, `MIME-Version: 1.0
Date: `+date(d)+`
Subject: `+subj(cjk)+`
From: `+subj("山田 太郎")+` <yamada@example.jp>
To: ahmet.yilmaz@gmail.com
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: base64

`+b64lines([]byte("明日10時から会議です。\r\nToplantı yarın saat 10'da.\r\n"))+`
`)

	// 7. Delivery report: the original headers ride along as
	// text/rfc822-headers and must not be shown as the body.
	d = ago(40)
	add(Sample{Key: "dsn", FromName: "Mail Delivery Subsystem", FromAddr: "mailer-daemon@googlemail.com", Subject: "Delivery Status Notification (Failure)", Date: d, Attach: true,
		WantText: "could not be delivered"}, gmailTrace+`MIME-Version: 1.0
Date: `+date(d)+`
Subject: Delivery Status Notification (Failure)
From: Mail Delivery Subsystem <mailer-daemon@googlemail.com>
To: ahmet.yilmaz@gmail.com
Content-Type: multipart/report; boundary="dsn-1"; report-type=delivery-status

--dsn-1
Content-Type: multipart/related; boundary="rel-1"

--rel-1
Content-Type: multipart/alternative; boundary="alt-2"

--alt-2
Content-Type: text/plain; charset="UTF-8"

Address not found

Your message to yok@olmayan-alan.com could not be delivered. The domain was not found.
--alt-2
Content-Type: text/html; charset="UTF-8"

<html><body><h2>Address not found</h2><p>Your message to yok@olmayan-alan.com could not be delivered.</p></body></html>
--alt-2--
--rel-1--
--dsn-1
Content-Type: message/delivery-status

Reporting-MTA: dns; googlemail.com
Final-Recipient: rfc822; yok@olmayan-alan.com
Action: failed
Status: 5.1.2

--dsn-1
Content-Type: text/rfc822-headers

`+gmailTrace+`Subject: Merhaba
From: ahmet.yilmaz@gmail.com
To: yok@olmayan-alan.com

--dsn-1--
`)
	// 8. Forwarded message as message/rfc822 with no text of its own.
	d = ago(50)
	add(Sample{Key: "fwd-rfc822", FromName: "Ayşe Demir", FromAddr: "ayse@ornek.com", Subject: "İlt: Toplantı notları", Date: d, Attach: true,
		WantText: "Toplantı notları ektedir"}, `MIME-Version: 1.0
Date: `+date(d)+`
Subject: `+subj("İlt: Toplantı notları")+`
From: `+subj("Ayşe Demir")+` <ayse@ornek.com>
To: ahmet.yilmaz@gmail.com
Content-Type: multipart/mixed; boundary="f-1"

--f-1
Content-Type: message/rfc822

`+gmailTrace+`Subject: Toplantı notları
From: Can <can@ornek.com>
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: 8bit

Toplantı notları ektedir, göz atar mısın?
--f-1--
`)
	return out
}

// Text renders a sample summary (debugging aid).
func (s Sample) String() string { return fmt.Sprintf("%s <%s> %q", s.FromName, s.FromAddr, s.Subject) }

func enc1254(s string) string {
	b, _ := charmap.Windows1254.NewEncoder().String(s)
	return mime.BEncoding.Encode("windows-1254", b)
}
