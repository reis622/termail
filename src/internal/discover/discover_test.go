package discover

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupTable(t *testing.T) {
	cases := []struct {
		email, provider string
		auth            AuthKind
		imap            string
	}{
		{"ahmet.yilmaz@gmail.com", "Gmail", AuthAppPassword, "imap.gmail.com"},
		{"Ahmet@HOTMAIL.com", "Outlook", AuthMicrosoft, "outlook.office365.com"},
		{"x@outlook.com.tr", "Outlook", AuthMicrosoft, "outlook.office365.com"},
		{"x@yandex.com.tr", "Yandex", AuthAppPassword, "imap.yandex.com"},
		{"x@icloud.com", "iCloud", AuthAppPassword, "imap.mail.me.com"},
	}
	for _, c := range cases {
		s, ok := Lookup(c.email)
		if !ok || s.Provider != c.provider || s.Auth != c.auth || s.Incoming.Host != c.imap {
			t.Errorf("%s: got %+v ok=%v", c.email, s, ok)
		}
	}
	if _, ok := Lookup("a@firma.com.tr"); ok {
		t.Error("unknown domain matched")
	}
	if _, ok := Lookup("bozuk"); ok {
		t.Error("invalid address matched")
	}
	if s, _ := Lookup("a@gmail.com"); s.SaveSent {
		t.Error("gmail must not save sent copy itself")
	}
}

func TestByMX(t *testing.T) {
	s, ok := ByMX([]string{"aspmx.l.google.com."})
	if !ok || s.Incoming.Host != "imap.gmail.com" || s.Provider != "Google" {
		t.Fatalf("google: %+v", s)
	}
	s, ok = ByMX([]string{"firma-com-tr.mail.protection.outlook.com."})
	if !ok || s.Auth != AuthMicrosoft {
		t.Fatalf("m365: %+v", s)
	}
	if _, ok := ByMX([]string{"mx.firma.com.tr"}); ok {
		t.Fatal("false positive")
	}
}

const sampleAC = `<?xml version="1.0"?>
<clientConfig version="1.1"><emailProvider id="firma.com.tr">
 <displayShortName>Firma</displayShortName>
 <incomingServer type="pop3"><hostname>pop.firma.com.tr</hostname><port>995</port><socketType>SSL</socketType></incomingServer>
 <incomingServer type="imap"><hostname>imap.%EMAILDOMAIN%</hostname><port>993</port><socketType>SSL</socketType><authentication>password-cleartext</authentication></incomingServer>
 <outgoingServer type="smtp"><hostname>smtp.firma.com.tr</hostname><port>25</port><socketType>plain</socketType></outgoingServer>
 <outgoingServer type="smtp"><hostname>smtp.firma.com.tr</hostname><port>587</port><socketType>STARTTLS</socketType></outgoingServer>
</emailProvider></clientConfig>`

func TestParseAutoconfig(t *testing.T) {
	s, err := ParseAutoconfig([]byte(sampleAC), "ali@firma.com.tr")
	if err != nil {
		t.Fatal(err)
	}
	if s.Incoming != (Server{"imap.firma.com.tr", 993, TLS}) || s.Outgoing != (Server{"smtp.firma.com.tr", 587, StartTLS}) || s.Provider != "Firma" {
		t.Fatalf("%+v", s)
	}
}

type fakeRes struct {
	mx  map[string][]*net.MX
	srv map[string][]*net.SRV
}

func (f fakeRes) LookupMX(_ context.Context, n string) ([]*net.MX, error) {
	if v, ok := f.mx[n]; ok {
		return v, nil
	}
	return nil, errors.New("nx")
}
func (f fakeRes) LookupSRV(_ context.Context, svc, proto, n string) (string, []*net.SRV, error) {
	if v, ok := f.srv["_"+svc+"._"+proto+"."+n]; ok {
		return "", v, nil
	}
	return "", nil, errors.New("nx")
}

func TestDiscoverChain(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/firma.com.tr" {
			w.Write([]byte(sampleAC))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()
	d := &Discoverer{Resolver: fakeRes{mx: map[string][]*net.MX{"gw.com.tr": {{Host: "alt1.aspmx.l.google.com."}}},
		srv: map[string][]*net.SRV{"_imaps._tcp.srv.org": {{Target: "mail.srv.org.", Port: 993}}, "_submission._tcp.srv.org": {{Target: "mail.srv.org.", Port: 587}}}},
		HTTP: ts.Client(), ISPDB: ts.URL + "/", NoDomainAutoconfig: true,
		Probe: func(_ context.Context, a string) bool {
			return a == "imap.tahmin.net:993" || a == "smtp.tahmin.net:587"
		}}
	ctx := context.Background()
	if s, err := d.Discover(ctx, "a@gmail.com"); err != nil || s.Source != "table" {
		t.Fatalf("table: %+v %v", s, err)
	}
	if s, err := d.Discover(ctx, "a@gw.com.tr"); err != nil || s.Source != "mx" {
		t.Fatalf("mx: %+v %v", s, err)
	}
	if s, err := d.Discover(ctx, "a@firma.com.tr"); err != nil || s.Incoming.Host != "imap.firma.com.tr" {
		t.Fatalf("ispdb: %+v %v", s, err)
	}
	if s, err := d.Discover(ctx, "a@srv.org"); err != nil || s.Source != "srv" || s.Outgoing.Security != StartTLS {
		t.Fatalf("srv: %+v %v", s, err)
	}
	if s, err := d.Discover(ctx, "a@tahmin.net"); err != nil || s.Source != "guess" || s.Outgoing.Port != 587 {
		t.Fatalf("guess: %+v %v", s, err)
	}
	if _, err := d.Discover(ctx, "a@yok.invalid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestParseManual(t *testing.T) {
	s, _ := ParseManual("mail.firma.com", true)
	if s != (Server{"mail.firma.com", 993, TLS}) {
		t.Fatal(s)
	}
	s, _ = ParseManual("smtp.firma.com:587", false)
	if s.Security != StartTLS {
		t.Fatal(s)
	}
	if _, err := ParseManual("x:abc", true); err == nil {
		t.Fatal("want error")
	}
}

func TestGreetingOK(t *testing.T) {
	try := func(greet string, incoming bool) bool {
		a, b := net.Pipe()
		defer a.Close()
		go func() {
			if greet != "" {
				b.Write([]byte(greet))
			}
			b.Close()
		}()
		return greetingOK(a, incoming)
	}
	if !try("* OK IMAP4rev1 hazır\r\n", true) || !try("220 smtp.ornek.com ESMTP\r\n", false) {
		t.Fatal("real greetings rejected")
	}
	if try("", true) || try("HTTP/1.1 400 Bad Request\r\n", true) || try("* OK\r\n", false) {
		t.Fatal("non-mail answer accepted (open port alone must not count)")
	}
}
