package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestPKCE(t *testing.T) {
	v1, c1 := NewPKCE()
	v2, _ := NewPKCE()
	if len(v1) != 43 || v1 == v2 {
		t.Fatalf("verifier %q (len %d)", v1, len(v1))
	}
	for _, r := range v1 + c1 {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			t.Fatalf("not url-safe: %q", r)
		}
	}
	// RFC 7636 appendix B test vector
	if got := Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge %s", got)
	}
	if c1 != Challenge(v1) {
		t.Fatal("challenge mismatch")
	}
}

func TestAuthURLParams(t *testing.T) {
	cfg, err := Config(Microsoft, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "456b7094-6cbb-4a4b-8b76-6a56fb79768e" {
		t.Fatalf("client id %q", cfg.ClientID)
	}
	u, _ := url.Parse(AuthURL(cfg, "http://localhost:50123", "st4te", "chal", "ahmet@hotmail.com"))
	if u.Scheme+"://"+u.Host+u.Path != "https://login.microsoftonline.com/common/oauth2/v2.0/authorize" {
		t.Fatalf("endpoint %s", u)
	}
	q := u.Query()
	want := map[string]string{
		"client_id": cfg.ClientID, "response_type": "code", "redirect_uri": "http://localhost:50123",
		"state": "st4te", "code_challenge": "chal", "code_challenge_method": "S256",
		"login_hint": "ahmet@hotmail.com", "response_mode": "query",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	sc := strings.Fields(q.Get("scope"))
	for _, s := range []string{"https://outlook.office.com/IMAP.AccessAsUser.All", "https://outlook.office.com/SMTP.Send", "offline_access", "openid", "email"} {
		if !contains(sc, s) {
			t.Errorf("scope %s missing in %v", s, sc)
		}
	}
	if q.Get("client_secret") != "" {
		t.Error("public client must not send a secret")
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// fakeTokenServer is a stand-in for Microsoft's token endpoint.
type fakeTokenServer struct {
	mu       sync.Mutex
	calls    []url.Values
	expires  int
	rotate   bool
	fail     string // error code to answer with
	verifier map[string]string
	n        int
}

func (f *fakeTokenServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.PostForm)
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/devicecode" {
		json.NewEncoder(w).Encode(map[string]any{"device_code": "DEV", "user_code": "ABCD1234", "verification_uri": "https://microsoft.com/devicelogin", "expires_in": 900, "interval": 1})
		return
	}
	if f.fail != "" {
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":"%s","error_description":"AADSTS70000: The provided grant has expired."}`, f.fail)
		return
	}
	f.n++
	rt := r.PostForm.Get("refresh_token")
	if f.rotate || rt == "" {
		rt = fmt.Sprintf("refresh-%d", f.n)
	}
	json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("access-%d", f.n), "token_type": "Bearer",
		"expires_in": f.expires, "refresh_token": rt, "id_token": fakeIDToken("Ahmet@Outlook.com")})
}

func fakeIDToken(email string) string {
	p, _ := json.Marshal(map[string]string{"preferred_username": email})
	return "e30." + base64.RawURLEncoding.EncodeToString(p) + ".sig"
}

func fakeConfig(srv *httptest.Server) *oauth2.Config {
	return &oauth2.Config{ClientID: "cid", Scopes: MSScopes, Endpoint: oauth2.Endpoint{
		AuthURL: srv.URL + "/authorize", TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode", AuthStyle: oauth2.AuthStyleInParams}}
}

func TestTokenRefresh(t *testing.T) {
	f := &fakeTokenServer{expires: 3600, rotate: true}
	srv := httptest.NewServer(f)
	defer srv.Close()
	var saved []string
	ts := TokenSource(fakeConfig(srv), "refresh-0", func(n string) { saved = append(saved, n) })
	t1, err := ts.Token()
	if err != nil || t1.AccessToken != "access-1" {
		t.Fatalf("%v %v", t1, err)
	}
	if got := f.calls[0]; got.Get("grant_type") != "refresh_token" || got.Get("refresh_token") != "refresh-0" || got.Get("client_id") != "cid" || got.Get("client_secret") != "" {
		t.Fatalf("refresh request %v", got)
	}
	if len(saved) != 1 || saved[0] != "refresh-1" {
		t.Fatalf("rotated refresh token not saved: %v", saved)
	}
	// still valid: reused, no second request
	ts.Token()
	if len(f.calls) != 1 {
		t.Fatalf("valid token renewed needlessly (%d calls)", len(f.calls))
	}
	// expired tokens are renewed with the newest refresh token
	f.expires = 1 // oauth2 renews 10s before expiry -> immediately stale
	ts2 := TokenSource(fakeConfig(srv), "refresh-1", func(n string) { saved = append(saved, n) })
	ts2.Token()
	ts2.Token()
	if len(f.calls) != 3 || f.calls[2].Get("refresh_token") != "refresh-2" {
		t.Fatalf("expired token not renewed with the rotated refresh token: %d calls", len(f.calls))
	}
	// revoked sign-in -> plain message
	f.fail = "invalid_grant"
	_, err = TokenSource(fakeConfig(srv), "old", nil).Token()
	if msg, _ := Friendly(err); msg != "Microsoft oturumunun süresi doldu; yeniden giriş yap." {
		t.Fatalf("revoked: %q (%v)", msg, err)
	}
}

func TestTokenSourceFromSeedNoRenew(t *testing.T) {
	f := &fakeTokenServer{expires: 3600}
	srv := httptest.NewServer(f)
	defer srv.Close()
	seed := &oauth2.Token{AccessToken: "fresh", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	tk, _ := TokenSourceFrom(fakeConfig(srv), seed, nil).Token()
	if tk.AccessToken != "fresh" || len(f.calls) != 0 {
		t.Fatal("seeded token not reused")
	}
}

// Full browser flow against a fake authority: the "browser" follows the
// sign-in URL, gets redirected back to the loopback address with a code; the
// code is exchanged with the matching PKCE verifier.
func TestBrowserLoginLoopback(t *testing.T) {
	f := &fakeTokenServer{expires: 3600}
	mux := http.NewServeMux()
	var challenge string
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		challenge = q.Get("code_challenge")
		http.Redirect(w, r, q.Get("redirect_uri")+"/?code=THECODE&state="+q.Get("state"), http.StatusFound)
	})
	mux.Handle("/token", f)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var page string
	open := func(u string) error {
		pu, _ := url.Parse(u)
		if !strings.HasPrefix(pu.Query().Get("redirect_uri"), "http://localhost:") {
			t.Errorf("redirect %s", pu.Query().Get("redirect_uri"))
		}
		go func() {
			u = strings.Replace(u, "localhost", "127.0.0.1", 1) // test box resolver
			c := &http.Client{CheckRedirect: func(r *http.Request, _ []*http.Request) error {
				r.URL.Host = strings.Replace(r.URL.Host, "localhost", "127.0.0.1", 1)
				return nil
			}}
			resp, err := c.Get(u)
			if err == nil {
				b, _ := io.ReadAll(resp.Body)
				page = string(b)
				resp.Body.Close()
			}
		}()
		return errors.New("no browser") // must not abort the flow
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := BrowserLogin(ctx, fakeConfig(srv), "ahmet@outlook.com", open)
	if err != nil {
		t.Fatal(err)
	}
	ex := f.calls[0]
	if ex.Get("grant_type") != "authorization_code" || ex.Get("code") != "THECODE" || Challenge(ex.Get("code_verifier")) != challenge {
		t.Fatalf("exchange %v", ex)
	}
	if tok.RefreshToken == "" || IDTokenEmail(tok) != "Ahmet@Outlook.com" {
		t.Fatal("token / address missing")
	}
	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(page, "Giriş tamam, Termail'e dönebilirsin") {
		t.Fatalf("success page: %q", page)
	}
}

func TestBrowserLoginErrorFromMicrosoft(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		http.Redirect(w, r, q.Get("redirect_uri")+"/?error=invalid_request&error_description="+url.QueryEscape("AADSTS50011: The redirect URI does not match.")+"&state="+q.Get("state"), http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	open := func(u string) error {
		go func() {
			c := &http.Client{CheckRedirect: func(r *http.Request, _ []*http.Request) error {
				r.URL.Host = strings.Replace(r.URL.Host, "localhost", "127.0.0.1", 1)
				return nil
			}}
			if resp, err := c.Get(u); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := BrowserLogin(ctx, fakeConfig(srv), "", open)
	msg, dev := Friendly(err)
	if !dev || !strings.Contains(msg, "Kodla girişe geçiliyor") {
		t.Fatalf("AADSTS50011 -> %q device=%v", msg, dev)
	}
}

func TestDeviceFlow(t *testing.T) {
	f := &fakeTokenServer{expires: 3600}
	srv := httptest.NewServer(f)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d, err := StartDevice(ctx, fakeConfig(srv))
	if err != nil || d.Code != "ABCD1234" || d.URL != "https://microsoft.com/devicelogin" {
		t.Fatalf("%+v %v", d, err)
	}
	tok, err := WaitDevice(ctx, fakeConfig(srv), d)
	if err != nil || tok.AccessToken == "" {
		t.Fatal(err)
	}
	if g := f.calls[len(f.calls)-1].Get("grant_type"); g != "urn:ietf:params:oauth:grant-type:device_code" {
		t.Fatalf("grant %s", g)
	}
}

func TestFriendlyErrors(t *testing.T) {
	cases := []struct {
		err  error
		want string
		dev  bool
	}{
		{parseErr("invalid_request", "AADSTS50011: The redirect URI 'http://localhost:1' does not match"), "Kodla girişe geçiliyor", true},
		{parseErr("invalid_client", "AADSTS7000218: The request body must contain client_assertion or client_secret"), "izin vermiyor", true},
		{parseErr("access_denied", "The user has denied access"), "iptal", false},
		{parseErr("invalid_grant", "AADSTS70008: expired"), "süresi doldu", false},
		{parseErr("unauthorized_client", "AADSTS700016: Application not found"), "kişisel hesaplara kapalı", false},
		{parseErr("unauthorized_client", "The client does not exist or is not enabled for consumers. If you are the application developer, configure a new application through the App Registrations in the Azure Portal"), "Uygulama kaydında kişisel hesapları aç.", false},
		{wrapOAuth(&oauth2.RetrieveError{ErrorCode: "invalid_client", ErrorDescription: "AADSTS7000218: ..."}), "izin vermiyor", true},
	}
	for _, c := range cases {
		msg, dev := Friendly(c.err)
		if !strings.Contains(msg, c.want) || dev != c.dev {
			t.Errorf("%v -> %q %v", c.err, msg, dev)
		}
		for _, bad := range []string{"AADSTS", "OAuth", "token", "redirect", "client"} {
			if strings.Contains(msg, bad) {
				t.Errorf("technical word in %q", msg)
			}
		}
	}
	if m, _ := Friendly(errors.New("x")); m != "" {
		t.Error("non-sign-in errors must pass through")
	}
}

func TestXOAUTH2String(t *testing.T) {
	c := NewXOAUTH2("ahmet@outlook.com", "ya29.TOKEN")
	mech, ir, err := c.Start()
	if err != nil || mech != "XOAUTH2" {
		t.Fatal(mech, err)
	}
	if string(ir) != "user=ahmet@outlook.com\x01auth=Bearer ya29.TOKEN\x01\x01" {
		t.Fatalf("%q", ir)
	}
	// on failure the server sends a JSON challenge; we answer empty
	if r, err := c.Next([]byte(`{"status":"401"}`)); err != nil || len(r) != 0 {
		t.Fatal("challenge answer")
	}
}

func TestSessionExpired(t *testing.T) {
	if !SessionExpired(fmt.Errorf("connect: %w", parseErr("invalid_grant", "AADSTS70008"))) || SessionExpired(errors.New("dial tcp: timeout")) {
		t.Fatal("SessionExpired")
	}
}
