package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"golang.org/x/oauth2"
	"termail/internal/i18n"
)

// DefaultMSClientID is the Microsoft (Azure) application ID used for
// Outlook / Hotmail / Microsoft 365 sign-in. It is a public client ID, not a
// secret. It can also be set at build time with
//
//	go build -ldflags "-X termail/internal/auth.DefaultMSClientID=<uygulama-kimliği>"
//
// (For testing it can be overridden with TERMAIL_MS_CLIENT_ID or the
// "microsoft_client_id" key in config.json.)
var DefaultMSClientID = "456b7094-6cbb-4a4b-8b76-6a56fb79768e"

// Authority supports personal (Outlook/Hotmail/Live) and work/school accounts.
const Authority = "https://login.microsoftonline.com/common"

// MSEndpoint is the Microsoft identity platform (v2) endpoint.
var MSEndpoint = oauth2.Endpoint{
	AuthURL:       Authority + "/oauth2/v2.0/authorize",
	TokenURL:      Authority + "/oauth2/v2.0/token",
	DeviceAuthURL: Authority + "/oauth2/v2.0/devicecode",
	AuthStyle:     oauth2.AuthStyleInParams, // public client: no secret
}

// MSScopes: mailbox access + refresh token + the signed-in address.
var MSScopes = []string{
	"https://outlook.office.com/IMAP.AccessAsUser.All",
	"https://outlook.office.com/SMTP.Send",
	"offline_access", "openid", "email", "profile",
}

// MSEnabled reports whether this build can sign in to Microsoft accounts.
func MSEnabled(configID string) bool {
	return firstNonEmpty(configID, os.Getenv("TERMAIL_MS_CLIENT_ID"), DefaultMSClientID) != ""
}

// Provider is a sign-in provider.
type Provider string

const (
	Microsoft Provider = "microsoft"
)

// ErrNoClientID means this build has no registered application for the provider.
var ErrNoClientID = errors.New("client id missing")

// ConfigFor is Config with the authority chosen for the address: personal
// Microsoft addresses (outlook.com, hotmail.*, live.*, msn.com) sign in at
// /consumers, which skips the work/school account lookup that can answer
// "account not found"; everything else (custom domains, work/school) at
// /common.
func ConfigFor(p Provider, clientID, email string) (*oauth2.Config, error) {
	c, err := Config(p, clientID)
	if err != nil || !IsConsumer(email) {
		return c, err
	}
	c.Endpoint = ConsumersEndpoint
	return c, nil
}

// ConsumersEndpoint is the personal-account (Outlook.com) authority.
var ConsumersEndpoint = oauth2.Endpoint{
	AuthURL:       ConsumersAuthority + "/oauth2/v2.0/authorize",
	TokenURL:      ConsumersAuthority + "/oauth2/v2.0/token",
	DeviceAuthURL: ConsumersAuthority + "/oauth2/v2.0/devicecode",
	AuthStyle:     oauth2.AuthStyleInParams,
}

// ConsumersAuthority signs in personal Microsoft accounts only.
const ConsumersAuthority = "https://login.microsoftonline.com/consumers"

// IsConsumer: a personal Microsoft address (outlook.com, outlook.com.tr,
// hotmail.co.uk, live.com, msn.com …).
func IsConsumer(email string) bool {
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return false
	}
	d := strings.ToLower(email[at+1:])
	first, rest, ok := strings.Cut(d, ".")
	if !ok {
		return false
	}
	switch first {
	case "outlook", "hotmail", "live", "msn", "windowslive", "passport":
	default:
		return false
	}
	// rest is a country / generic ending: com, fr, co.uk, com.tr …
	parts := strings.Split(rest, ".")
	if len(parts) > 2 {
		return false
	}
	for _, p := range parts {
		if len(p) < 2 || len(p) > 3 {
			return false
		}
	}
	return true
}

// ValidAddress: a plain address that is safe as login_hint (printable
// ASCII or letters, one @, a dotted domain, no spaces or control chars).
func ValidAddress(a string) bool {
	if len(a) < 3 || len(a) > 254 || strings.Count(a, "@") != 1 {
		return false
	}
	local, dom, _ := strings.Cut(a, "@")
	if local == "" || !strings.Contains(dom, ".") || strings.HasPrefix(dom, ".") || strings.HasSuffix(dom, ".") || strings.Contains(dom, "..") {
		return false
	}
	for _, r := range a {
		if r <= ' ' || r == 0x7f || (r >= 0x80 && r < 0xa0) || strings.ContainsRune("<>()[]\\,;:\"", r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Config returns the OAuth2 configuration for p, or ErrNoClientID.
func Config(p Provider, clientID string) (*oauth2.Config, error) {
	switch p {
	case Microsoft:
		clientID = firstNonEmpty(clientID, os.Getenv("TERMAIL_MS_CLIENT_ID"), DefaultMSClientID)
		if clientID == "" {
			return nil, ErrNoClientID
		}
		return &oauth2.Config{ClientID: clientID, Endpoint: MSEndpoint, Scopes: append([]string(nil), MSScopes...)}, nil
	}
	return nil, errors.New("bilinmeyen sağlayıcı")
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---- PKCE ----

// NewPKCE returns a fresh code verifier (43 chars, RFC 7636) and its S256
// challenge.
func NewPKCE() (verifier, challenge string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	return verifier, Challenge(verifier)
}

// Challenge is the S256 code challenge for verifier.
func Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// AuthURL builds the sign-in address opened in the browser.
func AuthURL(cfg *oauth2.Config, redirect, state, challenge, loginHint string) string {
	c := *cfg
	c.RedirectURL = redirect
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("response_mode", "query"),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	}
	if h := strings.ToLower(strings.TrimSpace(loginHint)); ValidAddress(h) {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", h))
	}
	return c.AuthCodeURL(state, opts...)
}

// ---- browser sign-in (authorization code + PKCE, loopback redirect) ----

// BrowserLogin runs the authorization-code + PKCE flow with a loopback
// redirect (http://localhost:<random port>). open is called with the URL the
// user must visit; when it fails the flow keeps waiting, since the caller
// shows the URL on screen. It returns when the browser comes back, ctx ends,
// or Microsoft reports an error.
func BrowserLogin(ctx context.Context, cfg *oauth2.Config, loginHint string, open func(string) error) (*oauth2.Token, error) {
	return loopbackLogin(ctx, Microsoft, cfg, "localhost", func(redirect, state, challenge string) string {
		return AuthURL(cfg, redirect, state, challenge, loginHint)
	}, open)
}

// loopbackLogin is the shared browser sign-in: a one-shot web server on a
// random loopback port receives the authorization code, which is exchanged
// with the PKCE verifier. host is the redirect host name ("localhost" for
// Microsoft, "127.0.0.1" for Google).
func loopbackLogin(ctx context.Context, p Provider, cfg *oauth2.Config, host string, mkURL func(redirect, state, challenge string) string, open func(string) error) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	// browsers may resolve localhost to ::1 first; listen there too
	var ln6 net.Listener
	if host == "localhost" {
		if l6, err := net.Listen("tcp", fmt.Sprintf("[::1]:%d", port)); err == nil {
			ln6 = l6
			defer ln6.Close()
		}
	}
	redirect := fmt.Sprintf("http://%s:%d", host, port)
	verifier, challenge := NewPKCE()
	stateB := make([]byte, 16)
	rand.Read(stateB)
	state := hex.EncodeToString(stateB)
	authURL := mkURL(redirect, state, challenge)

	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.NotFound(w, r) // favicon etc.
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if e := q.Get("error"); e != "" {
			ae := parseErr(e, q.Get("error_description"))
			ae.Provider = p
			msg, _ := Friendly(ae)
			fmt.Fprintf(w, page, "#e06c75", "✕", i18n.T("Giriş tamamlanamadı"), html.EscapeString(msg))
			select {
			case ch <- result{err: ae}:
			default:
			}
			return
		}
		fmt.Fprintf(w, page, "#98c379", "✓", i18n.T("Giriş tamam, Termail'e dönebilirsin"), i18n.T("Bu sekmeyi kapatabilirsin."))
		select {
		case ch <- result{code: q.Get("code")}:
		default:
		}
	})}
	go srv.Serve(ln)
	if ln6 != nil {
		go srv.Serve(ln6)
	}
	defer srv.Close()
	_ = open(authURL) // a failure is fine: the address is shown on screen
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		c := *cfg
		c.RedirectURL = redirect
		tok, err := c.Exchange(ctx, r.code, oauth2.SetAuthURLParam("code_verifier", verifier))
		if err != nil {
			return nil, wrapFor(p, err)
		}
		return tok, nil
	}
}

const page = `<!doctype html><html lang="tr"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Termail</title>
<body style="margin:0;background:#1f2227;color:#d5d8dd;font-family:'Segoe UI',system-ui,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh">
<div style="background:#282c34;border:1px solid #3a3f4b;border-radius:12px;padding:40px 56px;text-align:center;max-width:520px">
<div style="font-size:15px;letter-spacing:2px;color:#61afef;margin-bottom:20px">◆ TERMAIL</div>
<div style="font-size:48px;color:%s;line-height:1">%s</div>
<h2 style="font-weight:600;margin:18px 0 8px">%s</h2>
<p style="color:#9da5b4;margin:0">%s</p></div></body></html>`

// ---- device code sign-in ----

// DeviceCode is shown to the user when the browser sign-in can't be used.
type DeviceCode struct {
	URL, Code string
	resp      *oauth2.DeviceAuthResponse
}

// StartDevice begins the device-code flow.
func StartDevice(ctx context.Context, cfg *oauth2.Config) (*DeviceCode, error) {
	r, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, wrapOAuth(err)
	}
	return &DeviceCode{URL: r.VerificationURI, Code: r.UserCode, resp: r}, nil
}

// WaitDevice waits until the user approves the device code.
func WaitDevice(ctx context.Context, cfg *oauth2.Config, d *DeviceCode) (*oauth2.Token, error) {
	t, err := cfg.DeviceAccessToken(ctx, d.resp)
	if err != nil {
		return nil, wrapOAuth(err)
	}
	return t, nil
}

// ---- refresh ----

// TokenSource returns an auto-refreshing token source: an access token is
// reused until shortly before it expires, then renewed with the refresh
// token. onRefresh is called when Microsoft hands out a new refresh token so
// it can be persisted (in the Windows credential store).
func TokenSource(cfg *oauth2.Config, refresh string, onRefresh func(string)) oauth2.TokenSource {
	return tokenSource(Microsoft, cfg, refresh, onRefresh)
}

func tokenSource(p Provider, cfg *oauth2.Config, refresh string, onRefresh func(string)) oauth2.TokenSource {
	base := cfg.TokenSource(context.Background(), &oauth2.Token{RefreshToken: refresh})
	return &notifying{p: p, src: oauth2.ReuseTokenSource(nil, base), last: refresh, cb: onRefresh}
}

type notifying struct {
	p    Provider
	src  oauth2.TokenSource
	last string
	cb   func(string)
}

func (n *notifying) Token() (*oauth2.Token, error) {
	t, err := n.src.Token()
	if err != nil {
		return nil, wrapFor(n.p, err)
	}
	if t.RefreshToken != "" && t.RefreshToken != n.last {
		n.last = t.RefreshToken
		if n.cb != nil {
			n.cb(t.RefreshToken)
		}
	}
	return t, nil
}

// IDTokenEmail returns the signed-in address from the token's id_token
// (only used to pick the right mailbox name; not a security check).
func IDTokenEmail(t *oauth2.Token) string {
	s, _ := t.Extra("id_token").(string)
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var c struct {
		Email     string `json:"email"`
		Preferred string `json:"preferred_username"`
	}
	if jsonUnmarshal(raw, &c) != nil {
		return ""
	}
	for _, v := range []string{c.Email, c.Preferred} {
		if strings.Contains(v, "@") {
			return v
		}
	}
	return ""
}

// ---- errors ----

// Error is a sign-in error reported by Microsoft or Google.
type Error struct {
	OAuth    string // e.g. invalid_grant, access_denied
	Code     string // e.g. AADSTS50011
	Desc     string
	Provider Provider // "" (= Microsoft) or Google
}

func (e *Error) Error() string { return strings.TrimSpace(e.OAuth + " " + e.Code + " " + e.Desc) }

func parseErr(oauthCode, desc string) *Error {
	e := &Error{OAuth: oauthCode, Desc: desc}
	if i := strings.Index(desc, "AADSTS"); i >= 0 {
		j := i + 6
		for j < len(desc) && desc[j] >= '0' && desc[j] <= '9' {
			j++
		}
		e.Code = desc[i:j]
	}
	return e
}

// wrapFor is wrapOAuth with the provider recorded, so the message names
// the right company.
func wrapFor(p Provider, err error) error {
	err = wrapOAuth(err)
	var e *Error
	if p != Microsoft && errors.As(err, &e) {
		e.Provider = p
	}
	return err
}

func wrapOAuth(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		code := re.ErrorCode
		if code == "" {
			code = "error"
		}
		return parseErr(code, re.ErrorDescription+" "+string(re.Body))
	}
	return err
}

// Friendly turns a sign-in error into plain Turkish. tryDevice says whether
// signing in with a code (device flow) is worth offering instead.
func Friendly(err error) (msg string, tryDevice bool) {
	var e *Error
	if !errors.As(err, &e) {
		return "", false
	}
	if e.Provider == Google {
		return googleFriendly(e), false
	}
	if e.Code == "AADSTS700016" || e.OAuth == "unauthorized_client" || strings.Contains(strings.ToLower(e.Desc), "not enabled for consumers") {
		return msConsumersOff, false
	}
	switch e.Code {
	case "AADSTS50011", "AADSTS500113": // redirect address not registered
		return i18n.T("Microsoft tarayıcıdan dönüşü kabul etmedi. Kodla girişe geçiliyor."), true
	case "AADSTS7000218", "AADSTS700025": // public client flows disabled
		return i18n.T("Microsoft bu programın girişine izin vermiyor; program ayarlarında bir eksik var."), true
	case "AADSTS50020", "AADSTS50194", "AADSTS9002331": // account type not allowed
		return i18n.T("Bu tür hesapla giriş şu an açık değil."), false
	case "AADSTS65004", "AADSTS65001": // consent declined / required
		return i18n.T("İzin verilmedi. Giriş için tarayıcıda izin vermen gerekiyor."), false
	case "AADSTS70000", "AADSTS70008", "AADSTS700082", "AADSTS50173", "AADSTS50133":
		return i18n.T("Microsoft oturumunun süresi doldu; yeniden giriş yap."), false
	}
	switch e.OAuth {
	case "access_denied", "authorization_declined":
		return i18n.T("Giriş iptal edildi."), false
	case "expired_token":
		return i18n.T("Kodun süresi doldu; yeniden dene."), false
	case "invalid_grant", "interaction_required", "login_required":
		return i18n.T("Microsoft oturumunun süresi doldu; yeniden giriş yap."), false
	case "invalid_client":
		return i18n.T("Microsoft bu programın girişine izin vermiyor; program ayarlarında bir eksik var."), true
	}
	return i18n.T("Microsoft ile giriş tamamlanamadı. Tekrar dene."), false
}

// TokenSourceFrom is TokenSource seeded with a token that is still valid,
// so the first use does not renew it needlessly.
func TokenSourceFrom(cfg *oauth2.Config, t *oauth2.Token, onRefresh func(string)) oauth2.TokenSource {
	return tokenSourceFrom(Microsoft, cfg, t, onRefresh)
}

func tokenSourceFrom(p Provider, cfg *oauth2.Config, t *oauth2.Token, onRefresh func(string)) oauth2.TokenSource {
	base := cfg.TokenSource(context.Background(), t)
	return &notifying{p: p, src: oauth2.ReuseTokenSource(t, base), last: t.RefreshToken, cb: onRefresh}
}

// SessionExpired reports whether the saved Microsoft sign-in can no longer
// be renewed, so the user has to sign in again.
func SessionExpired(err error) bool {
	msg, _ := Friendly(err)
	return msg != "" && strings.Contains(msg, i18n.T("süresi doldu; yeniden giriş yap"))
}

// msConsumersOff: the app registration does not allow personal accounts
// (signInAudience) or does not exist for them.
const msConsumersOff = "Microsoft uygulama ayarı kişisel hesaplara kapalı. Uygulama kaydında kişisel hesapları aç."
