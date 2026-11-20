// Package discover finds mail server settings from an e-mail address:
// known provider table -> MX based guess -> Mozilla autoconfig / ISPDB ->
// DNS SRV -> common host names.
package discover

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Security of a server connection.
type Security string

const (
	TLS      Security = "tls"      // implicit TLS (993 / 465)
	StartTLS Security = "starttls" // upgrade (143 / 587)
	None     Security = "none"     // only allowed for local test servers
)

// AuthKind is how the user signs in.
type AuthKind string

const (
	AuthPassword    AuthKind = "password"
	AuthAppPassword AuthKind = "app-password" // password, but show the app-password hint
	AuthMicrosoft   AuthKind = "microsoft"
)

// Server is one endpoint.
type Server struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Security Security `json:"security"`
}

func (s Server) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// Settings is the result of discovery.
type Settings struct {
	Provider string   `json:"provider"` // friendly name: "Gmail", "Outlook", "Diğer"
	Incoming Server   `json:"incoming"`
	Outgoing Server   `json:"outgoing"`
	Auth     AuthKind `json:"auth"`
	// SaveSent: append sent mails to the Sent folder (Gmail does it itself).
	SaveSent bool   `json:"save_sent"`
	Source   string `json:"-"` // where it was found (for logs/tests only)
}

type provider struct {
	name    string
	domains []string
	mx      []string // MX host suffixes
	set     Settings
}

func std(name, imapHost, smtpHost string, auth AuthKind, saveSent bool) Settings {
	return Settings{Provider: name, Incoming: Server{imapHost, 993, TLS}, Outgoing: Server{smtpHost, 465, TLS}, Auth: auth, SaveSent: saveSent}
}

var providers = []provider{
	{"Gmail", []string{"gmail.com", "googlemail.com"}, []string{"google.com", "googlemail.com"},
		std("Gmail", "imap.gmail.com", "smtp.gmail.com", AuthAppPassword, false)},
	{"Outlook", []string{"outlook.com", "outlook.com.tr", "hotmail.com", "hotmail.com.tr", "hotmail.co.uk", "hotmail.fr", "hotmail.de", "hotmail.it", "live.com", "live.co.uk", "live.fr", "msn.com", "windowslive.com"},
		[]string{"protection.outlook.com", "outlook.com"},
		Settings{Provider: "Outlook", Incoming: Server{"outlook.office365.com", 993, TLS}, Outgoing: Server{"smtp.office365.com", 587, StartTLS}, Auth: AuthMicrosoft, SaveSent: false}},
	{"Yahoo", []string{"yahoo.com", "yahoo.co.uk", "yahoo.fr", "yahoo.de", "ymail.com", "rocketmail.com"}, []string{"yahoodns.net"},
		std("Yahoo", "imap.mail.yahoo.com", "smtp.mail.yahoo.com", AuthAppPassword, false)},
	{"Yandex", []string{"yandex.com", "yandex.com.tr", "yandex.ru", "ya.ru"}, []string{"yandex.net", "yandex.ru"},
		std("Yandex", "imap.yandex.com", "smtp.yandex.com", AuthAppPassword, true)},
	{"iCloud", []string{"icloud.com", "me.com", "mac.com"}, []string{"icloud.com"},
		Settings{Provider: "iCloud", Incoming: Server{"imap.mail.me.com", 993, TLS}, Outgoing: Server{"smtp.mail.me.com", 587, StartTLS}, Auth: AuthAppPassword, SaveSent: true}},
	{"GMX", []string{"gmx.com", "gmx.net", "gmx.de"}, []string{"gmx.net"},
		std("GMX", "imap.gmx.com", "mail.gmx.com", AuthPassword, true)},
	{"Zoho", []string{"zoho.com", "zohomail.com"}, []string{"zoho.com", "zoho.eu"},
		std("Zoho", "imap.zoho.com", "smtp.zoho.com", AuthPassword, true)},
	{"AOL", []string{"aol.com"}, []string{"aol.com"},
		std("AOL", "imap.aol.com", "smtp.aol.com", AuthAppPassword, true)},
	{"Mail.ru", []string{"mail.ru", "inbox.ru", "list.ru", "bk.ru"}, []string{"mail.ru"},
		std("Mail.ru", "imap.mail.ru", "smtp.mail.ru", AuthAppPassword, true)},
	{"Proton", []string{"proton.me", "protonmail.com", "pm.me"}, nil,
		Settings{Provider: "Proton", Incoming: Server{"127.0.0.1", 1143, StartTLS}, Outgoing: Server{"127.0.0.1", 1025, StartTLS}, Auth: AuthPassword, SaveSent: false}},
	{"Superonline", []string{"superonline.com", "superonline.net"}, nil,
		std("Superonline", "mail.superonline.com", "smtp.superonline.com", AuthPassword, true)},
	{"Turkcell", []string{"turkcell.com.tr"}, nil,
		std("Turkcell", "mail.turkcell.com.tr", "smtp.turkcell.com.tr", AuthPassword, true)},
}

// Domain returns the lower-cased domain part of an address ("" if invalid).
func Domain(email string) string {
	email = strings.TrimSpace(email)
	i := strings.LastIndex(email, "@")
	if i <= 0 || i == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[i+1:])
}

// Lookup checks only the built-in table (instant, no network).
func Lookup(email string) (Settings, bool) {
	d := Domain(email)
	if d == "" {
		return Settings{}, false
	}
	for _, p := range providers {
		for _, pd := range p.domains {
			if d == pd {
				s := p.set
				s.Source = "table"
				return s, true
			}
		}
	}
	return Settings{}, false
}

// ByMX maps MX host names to a known provider (Google Workspace, Microsoft 365...).
func ByMX(mxHosts []string) (Settings, bool) {
	for _, h := range mxHosts {
		h = strings.ToLower(strings.TrimSuffix(h, "."))
		for _, p := range providers {
			for _, suf := range p.mx {
				if h == suf || strings.HasSuffix(h, "."+suf) {
					s := p.set
					s.Source = "mx"
					if p.name == "Gmail" {
						s.Provider = "Google"
					}
					if p.name == "Outlook" {
						s.Provider = "Microsoft 365"
					}
					return s, true
				}
			}
		}
	}
	return Settings{}, false
}

// Resolver abstracts network lookups so discovery is testable.
type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
}

// Discoverer runs the full discovery chain.
type Discoverer struct {
	Resolver Resolver
	HTTP     *http.Client
	// Probe tests whether a TCP endpoint accepts connections.
	Probe func(ctx context.Context, addr string) bool
	// ISPDB base URL (Thunderbird's public database).
	ISPDB string
	// NoDomainAutoconfig skips autoconfig.<domain> lookups (tests).
	NoDomainAutoconfig bool
}

// New returns a Discoverer using the real network.
func New() *Discoverer {
	return &Discoverer{
		Resolver: net.DefaultResolver,
		HTTP:     &http.Client{Timeout: 6 * time.Second},
		Probe:    probeGreeting,
		ISPDB:    "https://autoconfig.thunderbird.net/v1.1/",
	}
}

// probeGreeting checks that a real mail server answers at addr: it must
// send a proper greeting ("* OK" for incoming mail, "220" for outgoing),
// over TLS on the encrypted ports. A bare open port is not enough (some
// networks accept every connection).
func probeGreeting(ctx context.Context, addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	d := net.Dialer{Timeout: 4 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
	var c net.Conn = raw
	if port == "993" || port == "465" {
		tc := tls.Client(raw, &tls.Config{ServerName: host})
		if err := tc.HandshakeContext(ctx); err != nil {
			return false
		}
		c = tc
	}
	return greetingOK(c, port == "993" || port == "143")
}

// greetingOK reads the first line a server sends and checks it looks like a
// mail server greeting.
func greetingOK(c net.Conn, incoming bool) bool {
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return false
	}
	if incoming {
		return strings.HasPrefix(line, "* OK") || strings.HasPrefix(line, "* PREAUTH")
	}
	return strings.HasPrefix(line, "220")
}

// ErrNotFound means every method failed; ask the user for server names.
var ErrNotFound = errors.New("sunucu bulunamadı")

// Discover finds settings for email.
func (d *Discoverer) Discover(ctx context.Context, email string) (Settings, error) {
	if s, ok := Lookup(email); ok {
		return s, nil
	}
	dom := Domain(email)
	if dom == "" {
		return Settings{}, errors.New("geçersiz adres")
	}
	var mxHosts []string
	if d.Resolver != nil {
		if mxs, err := d.Resolver.LookupMX(ctx, dom); err == nil {
			for _, m := range mxs {
				mxHosts = append(mxHosts, m.Host)
			}
		}
	}
	if s, ok := ByMX(mxHosts); ok {
		return s, nil
	}
	if d.HTTP != nil {
		q := url.QueryEscape(email)
		var urls []string
		if !d.NoDomainAutoconfig {
			urls = append(urls,
				"https://autoconfig."+dom+"/mail/config-v1.1.xml?emailaddress="+q,
				"https://"+dom+"/.well-known/autoconfig/mail/config-v1.1.xml?emailaddress="+q)
		}
		if d.ISPDB != "" {
			urls = append(urls, d.ISPDB+dom)
		}
		for _, u := range urls {
			if s, err := d.fetchAutoconfig(ctx, u, email); err == nil {
				return s, nil
			}
		}
	}
	if d.Resolver != nil {
		if s, ok := d.srv(ctx, dom); ok {
			return s, nil
		}
	}
	if d.Probe != nil {
		cands := []string{"imap." + dom, "mail." + dom}
		for _, h := range cands {
			if d.Probe(ctx, net.JoinHostPort(h, "993")) {
				s := Settings{Provider: "Diğer", Incoming: Server{h, 993, TLS}, Auth: AuthPassword, SaveSent: true, Source: "guess"}
				for _, o := range []Server{{"smtp." + dom, 465, TLS}, {"smtp." + dom, 587, StartTLS}, {h, 465, TLS}, {h, 587, StartTLS}} {
					if d.Probe(ctx, o.Addr()) {
						s.Outgoing = o
						return s, nil
					}
				}
			}
		}
	}
	return Settings{}, ErrNotFound
}

func (d *Discoverer) srv(ctx context.Context, dom string) (Settings, bool) {
	_, in, err := d.Resolver.LookupSRV(ctx, "imaps", "tcp", dom)
	if err != nil || len(in) == 0 || in[0].Target == "." {
		return Settings{}, false
	}
	s := Settings{Provider: "Diğer", Incoming: Server{strings.TrimSuffix(in[0].Target, "."), int(in[0].Port), TLS}, Auth: AuthPassword, SaveSent: true, Source: "srv"}
	if _, out, err := d.Resolver.LookupSRV(ctx, "submissions", "tcp", dom); err == nil && len(out) > 0 && out[0].Target != "." {
		s.Outgoing = Server{strings.TrimSuffix(out[0].Target, "."), int(out[0].Port), TLS}
	} else if _, out, err := d.Resolver.LookupSRV(ctx, "submission", "tcp", dom); err == nil && len(out) > 0 && out[0].Target != "." {
		s.Outgoing = Server{strings.TrimSuffix(out[0].Target, "."), int(out[0].Port), StartTLS}
	} else {
		return Settings{}, false
	}
	return s, true
}

func (d *Discoverer) fetchAutoconfig(ctx context.Context, u, email string) (Settings, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Settings{}, err
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return Settings{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Settings{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Settings{}, err
	}
	s, err := ParseAutoconfig(b, email)
	if err == nil {
		s.Source = u
	}
	return s, err
}

type acServer struct {
	Type       string   `xml:"type,attr"`
	Hostname   string   `xml:"hostname"`
	Port       int      `xml:"port"`
	SocketType string   `xml:"socketType"`
	Auth       []string `xml:"authentication"`
}

type acDoc struct {
	Provider struct {
		ID       string     `xml:"id,attr"`
		Display  string     `xml:"displayShortName"`
		Incoming []acServer `xml:"incomingServer"`
		Outgoing []acServer `xml:"outgoingServer"`
	} `xml:"emailProvider"`
}

func secOf(sock string) (Security, bool) {
	switch strings.ToUpper(sock) {
	case "SSL":
		return TLS, true
	case "STARTTLS":
		return StartTLS, true
	}
	return "", false // plain connections are refused
}

// ParseAutoconfig parses a Mozilla autoconfig (config-v1.1.xml) document.
func ParseAutoconfig(b []byte, email string) (Settings, error) {
	var doc acDoc
	if err := xml.Unmarshal(b, &doc); err != nil {
		return Settings{}, err
	}
	s := Settings{Provider: doc.Provider.Display, Auth: AuthPassword, SaveSent: true}
	if s.Provider == "" {
		s.Provider = "Diğer"
	}
	found := false
	for _, in := range doc.Provider.Incoming {
		if !strings.EqualFold(in.Type, "imap") {
			continue
		}
		sec, ok := secOf(in.SocketType)
		if !ok {
			continue
		}
		s.Incoming = Server{expand(in.Hostname, email), in.Port, sec}
		for _, a := range in.Auth {
			if strings.EqualFold(a, "OAuth2") && isMicrosoftHost(in.Hostname) {
				s.Auth = AuthMicrosoft
			}
		}
		found = true
		break
	}
	if !found {
		return Settings{}, errors.New("no imap server")
	}
	for _, out := range doc.Provider.Outgoing {
		if !strings.EqualFold(out.Type, "smtp") {
			continue
		}
		sec, ok := secOf(out.SocketType)
		if !ok {
			continue
		}
		s.Outgoing = Server{expand(out.Hostname, email), out.Port, sec}
		return s, nil
	}
	return Settings{}, errors.New("no smtp server")
}

func isMicrosoftHost(h string) bool {
	h = strings.ToLower(h)
	return strings.HasSuffix(h, "office365.com") || strings.HasSuffix(h, "outlook.com")
}

func expand(h, email string) string {
	h = strings.ReplaceAll(h, "%EMAILDOMAIN%", Domain(email))
	return strings.TrimSpace(h)
}

// ParseManual turns a user-entered "host" or "host:port" into a server.
// Implicit TLS is assumed unless the port is 587/143.
func ParseManual(s string, incoming bool) (Server, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Server{}, errors.New("boş")
	}
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		host = s
		if incoming {
			portStr = "993"
		} else {
			portStr = "465"
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return Server{}, errors.New("geçersiz")
	}
	sec := TLS
	if port == 587 || port == 143 || port == 25 {
		sec = StartTLS
	}
	// Developer aid: plain connections to a local test server.
	if os.Getenv("TERMAIL_DEV_PLAIN_LOCAL") == "1" && (host == "127.0.0.1" || host == "localhost") {
		sec = None
	}
	return Server{host, port, sec}, nil
}
