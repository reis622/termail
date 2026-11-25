package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"termail/internal/account"
	"termail/internal/auth"
	"termail/internal/discover"
	"termail/internal/i18n"
)

// clickKeybar clicks the bottom-bar hint whose key string is s.
func clickKeybar(t *testing.T, m *Model, s string) {
	t.Helper()
	_ = m.View()
	for _, h := range m.hits.list {
		if h.kind == hitKeybar && h.s == s {
			m.Update(tea.MouseMsg{X: h.x, Y: h.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			return
		}
	}
	t.Fatalf("no clickable %q on screen", s)
}

func TestGmailShowsGoogleSignIn(t *testing.T) {
	b := stubBrowser(t, false)
	m, l := loginModelFor(t)
	typeText(m, "ahmet@gmail.com")
	if !l.isGoogle() || l.isMS() || len(l.inputs()) != 1 {
		t.Fatal("a Gmail address must use Google sign-in with only the address field")
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"Google hesabı için şifre gerekmez", "yapılandırılmamış", "Ctrl+P", "Uygulama şifresiyle gir"} {
		if !strings.Contains(v, want) {
			t.Fatalf("%q missing on screen", want)
		}
	}
	for _, bad := range []string{"Şifre", "OAuth", "IMAP", "SMTP", "token"} {
		if strings.Contains(v, bad) {
			t.Fatalf("%q on the Google screen", bad)
		}
	}
	// no client ID: Enter explains, nothing starts
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if l.busy || l.waiting || len(b.opened) != 0 {
		t.Fatal("Google sign-in started without a client")
	}
	if !strings.Contains(l.status, "Google ile giriş bu sürümde yapılandırılmamış") {
		t.Fatalf("status %q", l.status)
	}
}

func TestGmailAppPasswordFallbackKeyAndMouse(t *testing.T) {
	stubBrowser(t, false)
	m, l := loginModelFor(t)
	typeText(m, "ahmet@gmail.com")
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlP})
	if l.isGoogle() || !l.usePass || l.focus != 1 || !l.appPass() {
		t.Fatal("Ctrl+P must switch to the app password with the password focused")
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Şifre") || !strings.Contains(v, "Google ile gir") {
		t.Fatal("password view lacks the field or the way back")
	}
	clickKeybar(t, m, "ctrl+p") // mouse: back to Google
	if !l.isGoogle() || l.focus != 0 {
		t.Fatal("clicking Ctrl+P must switch back to Google")
	}
	clickKeybar(t, m, "ctrl+p")
	if l.isGoogle() {
		t.Fatal("clicking Ctrl+P must switch to the app password")
	}
	// other providers ignore the key
	m2, l2 := loginModelFor(t)
	typeText(m2, "ali@yahoo.com")
	m2.onKey(tea.KeyMsg{Type: tea.KeyCtrlP})
	if l2.usePass {
		t.Fatal("Ctrl+P changed a non-Google address")
	}
}

func TestHotmailStillMicrosoft(t *testing.T) {
	m, l := loginModelFor(t)
	typeText(m, "ahmet@hotmail.com")
	if !l.isMS() || l.isGoogle() {
		t.Fatal("hotmail must stay on Microsoft sign-in")
	}
	if strings.Contains(ansi.Strip(m.View()), "Ctrl+P") {
		t.Fatal("app-password switch shown for Microsoft")
	}
}

func TestGoogleSignInStartsWithClient(t *testing.T) {
	b := stubBrowser(t, false)
	m, l := loginModelFor(t)
	t.Setenv(auth.EnvGoogleClientID, "test-client-id")
	t.Setenv(auth.EnvGoogleClientSecret, "test-secret")
	typeText(m, "ahmet@gmail.com")
	v := ansi.Strip(m.View())
	if strings.Contains(v, "yapılandırılmamış") || !strings.Contains(v, "Tarayıcıda Google ile giriş yap") {
		t.Fatal("configured build must offer Google sign-in")
	}
	clickKeybar(t, m, "enter") // the mouse starts it too
	if !l.busy || !l.waiting {
		t.Fatal("Google sign-in not started")
	}
	v = ansi.Strip(m.View())
	if !strings.Contains(v, "Tarayıcıda onay bekleniyor") || strings.Contains(v, "kodla giriş") {
		t.Fatal("waiting box wrong for Google")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})
	if l.busy {
		t.Fatal("Esc must cancel")
	}
	_ = b
}

func TestGoogleDemoSignInAddsGoogleAccount(t *testing.T) {
	stubBrowser(t, false)
	m, l := loginModelFor(t)
	m.opt.Demo = true
	typeText(m, "ahmet@gmail.com")
	cmd := m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !l.waiting || !strings.HasPrefix(l.authURL, "https://accounts.google.com/") {
		t.Fatalf("demo Google sign-in not started (%q)", l.authURL)
	}
	s := googleSettings("ahmet@gmail.com")
	m.Update(loginDoneMsg{acct: account.Account{Email: "ahmet@gmail.com", Settings: s}, b: demoBackendFor("ahmet@gmail.com")})
	if m.login != nil || len(m.accts) != 1 || m.accts[0].Cfg.Settings.Auth != discover.AuthGoogle {
		t.Fatal("Google account not added")
	}
	if s.Incoming.Host != "imap.gmail.com" || s.Incoming.Port != 993 || s.Outgoing.Host != "smtp.gmail.com" {
		t.Fatalf("settings %+v", s)
	}
}

func TestGoogleLoginEnglish(t *testing.T) {
	i18n.Set(i18n.EN)
	t.Cleanup(func() { i18n.Set(i18n.TR) })
	m, _ := loginModelFor(t)
	typeText(m, "ahmet@gmail.com")
	v := ansi.Strip(m.View())
	for _, want := range []string{"A Google account does not need a password here.", "not configured", "Use an app password"} {
		if !strings.Contains(v, want) {
			t.Fatalf("%q missing in English", want)
		}
	}
}

func TestBuildBackendGoogle(t *testing.T) {
	account.UseMemoryKeyring()
	t.Setenv(auth.EnvGoogleClientID, "")
	t.Setenv(auth.EnvGoogleClientSecret, "")
	a := account.Account{Email: "ahmet@gmail.com", Settings: googleSettings("ahmet@gmail.com")}
	if err := account.SetSecret(a.Email, account.SecretRefresh, "r-token"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = account.DeleteSecret(a.Email, account.SecretRefresh) })
	if _, err := BuildBackend(&account.Config{}, a); !errors.Is(err, auth.ErrNoClientID) {
		t.Fatalf("no client: %v", err)
	}
	b, err := BuildBackend(&account.Config{GoogleClientID: "cid", GoogleClientSecret: "cs"}, a)
	if err != nil || b == nil {
		t.Fatalf("with client: %v", err)
	}
}

func TestGmailReloginSwitchesToGoogle(t *testing.T) {
	account.UseMemoryKeyring()
	t.Setenv("TERMAIL_CONFIG_DIR", t.TempDir())
	stubBrowser(t, false)
	old := account.Account{Name: "Gmail", Email: "ahmet@gmail.com"}
	old.Settings, _ = discover.Lookup("ahmet@gmail.com") // app password
	cfg := &account.Config{Accounts: []account.Account{old}}
	m := New(Options{Config: cfg, Accounts: []*Acct{{Cfg: old, State: stNeedsLogin}}})
	m.w, m.h = 140, 40
	m.login = newLogin(m, true)
	m.login.resize(m.w)
	typeText(m, "ahmet@gmail.com")
	m.login.submit()
	if m.login.relogin != 0 {
		t.Fatalf("relogin %d", m.login.relogin)
	}
	s := googleSettings("ahmet@gmail.com")
	m.Update(loginDoneMsg{acct: account.Account{Email: "ahmet@gmail.com", Settings: s}, b: demoBackendFor("x"), secretKind: account.SecretRefresh, secret: "r"})
	t.Cleanup(func() { _ = account.DeleteSecret("ahmet@gmail.com", account.SecretRefresh) })
	if m.accts[0].Cfg.Settings.Auth != discover.AuthGoogle || cfg.Accounts[0].Settings.Auth != discover.AuthGoogle {
		t.Fatal("account kept the app-password kind after Google sign-in")
	}
	loaded, err := account.Load()
	if err != nil || len(loaded.Accounts) != 1 || loaded.Accounts[0].Settings.Auth != discover.AuthGoogle {
		t.Fatalf("not saved: %+v %v", loaded, err)
	}
}
