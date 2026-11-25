package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"termail/internal/i18n"
)

func TestDetectProvider(t *testing.T) {
	cases := map[string]Provider{
		"ahmet@gmail.com":       Google,
		" Ahmet@GMAIL.com ":     Google,
		"a@googlemail.com":      Google,
		"a@hotmail.com":         Microsoft,
		"a@outlook.com.tr":      Microsoft,
		"a@live.com":            Microsoft,
		"a@msn.com":             Microsoft,
		"a@hotmail.co.uk":       Microsoft,
		"a@firma.com":           "",
		"a@gmail.com.example":   "",
		"a@mail.gmail.com":      "",
		"gmail.com":             "",
		"@gmail.com":            "",
		"a@yahoo.com":           "",
		"a@outlook.example.com": "",
	}
	for in, want := range cases {
		if got := DetectProvider(in); got != want {
			t.Errorf("DetectProvider(%q) = %q, want %q", in, got, want)
		}
	}
}

// withBuildClient sets the ldflags-injected values for one test.
func withBuildClient(t *testing.T, id, secret string) {
	t.Helper()
	oldID, oldSecret := googleClientID, googleClientSecret
	googleClientID, googleClientSecret = id, secret
	t.Cleanup(func() { googleClientID, googleClientSecret = oldID, oldSecret })
}

func TestGoogleMissingClientID(t *testing.T) {
	t.Setenv(EnvGoogleClientID, "")
	t.Setenv(EnvGoogleClientSecret, "")
	withBuildClient(t, "", "")
	if GoogleEnabled("", "") {
		t.Fatal("enabled without a client")
	}
	if _, err := GoogleConfig("", ""); !errors.Is(err, ErrNoClientID) {
		t.Fatalf("err %v", err)
	}
	// an ID without its secret is not enough for a Google desktop client
	t.Setenv(EnvGoogleClientID, "only-id")
	if GoogleEnabled("", "") {
		t.Fatal("enabled without a secret")
	}
	if !strings.Contains(GoogleUnavailable(), "yapılandırılmamış") {
		t.Fatalf("message %q", GoogleUnavailable())
	}
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.TR)
	if m := GoogleUnavailable(); !strings.Contains(m, "not configured") {
		t.Fatalf("english message %q", m)
	}
}

func TestGoogleCredentialSources(t *testing.T) {
	t.Setenv(EnvGoogleClientID, "")
	t.Setenv(EnvGoogleClientSecret, "")
	withBuildClient(t, "build-id", "build-secret")
	c, err := GoogleConfig("", "")
	if err != nil || c.ClientID != "build-id" || c.ClientSecret != "build-secret" {
		t.Fatalf("ldflags client: %+v %v", c, err)
	}
	if c.Endpoint.AuthURL != "https://accounts.google.com/o/oauth2/v2/auth" || c.Endpoint.TokenURL != "https://oauth2.googleapis.com/token" {
		t.Fatalf("endpoint %+v", c.Endpoint)
	}
	if !contains(c.Scopes, "https://mail.google.com/") {
		t.Fatalf("scopes %v", c.Scopes)
	}
	t.Setenv(EnvGoogleClientID, "env-id")
	t.Setenv(EnvGoogleClientSecret, "env-secret")
	if id, s := GoogleCredentials("", ""); id != "env-id" || s != "env-secret" {
		t.Fatalf("env: %s %s", id, s)
	}
	if id, s := GoogleCredentials("cfg-id", "cfg-secret"); id != "cfg-id" || s != "cfg-secret" {
		t.Fatalf("config: %s %s", id, s)
	}
	// the secret is never taken from a different source than the ID
	if id, s := GoogleCredentials("cfg-id", ""); id != "cfg-id" || s != "" {
		t.Fatalf("mixed pair: %s %s", id, s)
	}
}

func TestGoogleAuthURL(t *testing.T) {
	cfg := &oauth2.Config{ClientID: "gid", ClientSecret: "gsecret", Endpoint: GoogleEndpoint, Scopes: GoogleScopes}
	u, err := url.Parse(GoogleAuthURL(cfg, "http://127.0.0.1:50123", "st4te", "chal", " Ahmet@Gmail.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme+"://"+u.Host+u.Path != GoogleEndpoint.AuthURL {
		t.Fatalf("endpoint %s", u)
	}
	q := u.Query()
	want := map[string]string{
		"client_id": "gid", "response_type": "code", "redirect_uri": "http://127.0.0.1:50123",
		"state": "st4te", "code_challenge": "chal", "code_challenge_method": "S256",
		"access_type": "offline", "login_hint": "ahmet@gmail.com",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if !strings.Contains(q.Get("prompt"), "consent") {
		t.Errorf("prompt %q", q.Get("prompt"))
	}
	if !contains(strings.Fields(q.Get("scope")), "https://mail.google.com/") {
		t.Errorf("scope %q", q.Get("scope"))
	}
	if q.Get("client_secret") != "" || q.Get("response_mode") != "" {
		t.Errorf("unexpected params %v", q)
	}
}

func fakeGoogleConfig(srv *httptest.Server) *oauth2.Config {
	return &oauth2.Config{ClientID: "gid", ClientSecret: "gsecret", Scopes: GoogleScopes, Endpoint: oauth2.Endpoint{
		AuthURL: srv.URL + "/authorize", TokenURL: srv.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}
}

func TestGoogleBrowserLoginLoopback(t *testing.T) {
	f := &fakeTokenServer{expires: 3600}
	mux := http.NewServeMux()
	var challenge, method string
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		challenge, method = q.Get("code_challenge"), q.Get("code_challenge_method")
		http.Redirect(w, r, q.Get("redirect_uri")+"/?code=GCODE&scope=https://mail.google.com/&state="+q.Get("state"), http.StatusFound)
	})
	mux.Handle("/token", f)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page := make(chan string, 1)
	open := func(u string) error {
		pu, _ := url.Parse(u)
		if !strings.HasPrefix(pu.Query().Get("redirect_uri"), "http://127.0.0.1:") {
			t.Errorf("redirect %s", pu.Query().Get("redirect_uri"))
		}
		go func() {
			resp, err := http.Get(u)
			if err == nil {
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				page <- string(b)
			}
		}()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := GoogleBrowserLogin(ctx, fakeGoogleConfig(srv), "ahmet@gmail.com", open)
	if err != nil {
		t.Fatal(err)
	}
	ex := f.calls[0]
	if ex.Get("grant_type") != "authorization_code" || ex.Get("code") != "GCODE" || method != "S256" || Challenge(ex.Get("code_verifier")) != challenge {
		t.Fatalf("exchange %v", ex)
	}
	if ex.Get("client_id") != "gid" || ex.Get("client_secret") != "gsecret" || !strings.HasPrefix(ex.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatalf("client / redirect in exchange %v", ex)
	}
	if tok.RefreshToken == "" || tok.AccessToken == "" {
		t.Fatal("token missing")
	}
	select {
	case p := <-page:
		if !strings.Contains(p, "Giriş tamam") {
			t.Fatalf("success page %q", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no success page")
	}
}

func TestGoogleDeniedInBrowser(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		http.Redirect(w, r, q.Get("redirect_uri")+"/?error=access_denied&state="+q.Get("state"), http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	open := func(u string) error {
		go func() {
			if resp, err := http.Get(u); err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := GoogleBrowserLogin(ctx, fakeGoogleConfig(srv), "", open)
	msg, dev := Friendly(err)
	if msg != "Giriş iptal edildi." || dev {
		t.Fatalf("denied -> %q %v (%v)", msg, dev, err)
	}
}

func TestGoogleTokenRefresh(t *testing.T) {
	f := &fakeTokenServer{expires: 3600, rotate: true}
	srv := httptest.NewServer(f)
	defer srv.Close()
	var saved []string
	ts := GoogleTokenSource(fakeGoogleConfig(srv), "g-refresh-0", func(n string) { saved = append(saved, n) })
	tk, err := ts.Token()
	if err != nil || tk.AccessToken != "access-1" {
		t.Fatalf("%v %v", tk, err)
	}
	c := f.calls[0]
	if c.Get("grant_type") != "refresh_token" || c.Get("refresh_token") != "g-refresh-0" || c.Get("client_secret") != "gsecret" {
		t.Fatalf("refresh request %v", c)
	}
	if len(saved) != 1 {
		t.Fatalf("rotated refresh token not saved: %v", saved)
	}
	ts.Token()
	if len(f.calls) != 1 {
		t.Fatal("valid token renewed needlessly")
	}
	// revoked: Google wording, and the account asks to sign in again
	f.fail = "invalid_grant"
	_, err = GoogleTokenSource(fakeGoogleConfig(srv), "old", nil).Token()
	if msg, _ := Friendly(err); msg != "Google oturumunun süresi doldu; yeniden giriş yap." {
		t.Fatalf("revoked: %q (%v)", msg, err)
	}
	if !SessionExpired(err) {
		t.Fatal("revoked Google sign-in must ask to sign in again")
	}
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.TR)
	if !SessionExpired(err) {
		t.Fatal("SessionExpired in English")
	}
}

func TestGoogleFriendlyPlainWords(t *testing.T) {
	for _, code := range []string{"access_denied", "invalid_grant", "invalid_client", "unauthorized_client", "admin_policy_enforced", "org_internal", "server_error"} {
		msg, dev := Friendly(&Error{OAuth: code, Provider: Google})
		if msg == "" || dev {
			t.Errorf("%s -> %q %v", code, msg, dev)
		}
		if strings.Contains(msg, "Microsoft") {
			t.Errorf("%s names Microsoft: %q", code, msg)
		}
		for _, bad := range []string{"OAuth", "token", "redirect", "client"} {
			if strings.Contains(msg, bad) {
				t.Errorf("technical word in %q", msg)
			}
		}
	}
}

func TestXOAUTH2Gmail(t *testing.T) {
	_, ir, err := NewXOAUTH2("ahmet@gmail.com", "ya29.a0Abc").Start()
	if err != nil || string(ir) != "user=ahmet@gmail.com\x01auth=Bearer ya29.a0Abc\x01\x01" {
		t.Fatalf("%q %v", ir, err)
	}
}
