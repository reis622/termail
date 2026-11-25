package ui

import (
	"errors"
	"strings"
	"termail/internal/auth"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/account"
	"termail/internal/backend"
)

type browserStub struct {
	opened, copied []string
	fail           bool
}

func stubBrowser(t *testing.T, fail bool) *browserStub {
	t.Helper()
	b := &browserStub{fail: fail}
	oldO, oldC := openURLFn, copyTextFn
	openURLFn = func(u string) error {
		b.opened = append(b.opened, u)
		if b.fail {
			return errors.New("no browser")
		}
		return nil
	}
	copyTextFn = func(s string) { b.copied = append(b.copied, s) }
	t.Cleanup(func() { openURLFn, copyTextFn = oldO, oldC })
	return b
}

func loginModelFor(t *testing.T) (*Model, *loginModel) {
	t.Helper()
	t.Setenv("TERMAIL_MS_CLIENT_ID", "")
	t.Setenv("TERMAIL_GOOGLE_CLIENT_ID", "")
	t.Setenv("TERMAIL_GOOGLE_CLIENT_SECRET", "")
	m := New(Options{Config: &account.Config{}})
	m.w, m.h = 140, 40
	if m.login == nil {
		t.Fatal("no login screen without accounts")
	}
	m.login.resize(m.w)
	return m, m.login
}

func typeText(m *Model, s string) {
	for _, r := range s {
		m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

const gmailHelp = "https://myaccount.google.com/apppasswords"

func TestLoginCtrlOWhileEmailFocused(t *testing.T) {
	b := stubBrowser(t, false)
	m, l := loginModelFor(t)
	typeText(m, "ahmet@gmail.com")
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlP}) // app-password fallback
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	if len(b.opened) != 1 || b.opened[0] != gmailHelp {
		t.Fatalf("Ctrl+O opened %v", b.opened)
	}
	if l.email.Value() != "ahmet@gmail.com" {
		t.Fatalf("Ctrl+O leaked into the text field: %q", l.email.Value())
	}
	v := m.View()
	if !strings.Contains(v, gmailHelp) {
		t.Fatal("address not shown on screen after Ctrl+O")
	}
	if !strings.Contains(v, "Nasıl alınır?  Ctrl + O harfi") || !strings.Contains(v, "Ctrl + O harfi ") {
		t.Fatal("Ctrl+O not labelled as a letter in the hint / key bar")
	}
	if strings.Count(v, "F2") != 1 || !strings.Contains(v, "Şifreyi göster/gizle") {
		t.Fatal("F2 must only be the show-password key")
	}
}

func TestLoginCtrlOWhilePasswordFocused(t *testing.T) {
	b := stubBrowser(t, false)
	m, l := loginModelFor(t)
	typeText(m, "ahmet@gmail.com")
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlP}) // app-password fallback: focuses the password
	typeText(m, "abcd")
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	if len(b.opened) != 1 || l.pass.Value() != "abcd" || l.focus != 1 {
		t.Fatalf("Ctrl+O: opened=%v pass=%q focus=%d", b.opened, l.pass.Value(), l.focus)
	}
}

func TestLoginHelpWithoutBrowserStillShowsAddress(t *testing.T) {
	b := stubBrowser(t, true)
	m, l := loginModelFor(t)
	typeText(m, "ali@gmail.com")
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlP})
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	if l.helpURL != gmailHelp || len(b.copied) == 0 || b.copied[0] != gmailHelp {
		t.Fatal("address not kept / copied when the browser fails")
	}
	if !strings.Contains(m.View(), gmailHelp) || !strings.Contains(l.status, "Tarayıcı açılamadı") {
		t.Fatal("failure not explained on screen")
	}
}

func TestLoginCtrlGDoesNothing(t *testing.T) {
	b := stubBrowser(t, false)
	m, l := loginModelFor(t)
	typeText(m, "ahmet@gmail.com")
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlG})
	if l.busy || l.waiting || len(b.opened) != 0 {
		t.Fatal("Ctrl+G still starts something")
	}
}

func TestLoginMicrosoftEnabled(t *testing.T) {
	b := stubBrowser(t, false)
	m, l := loginModelFor(t)
	typeText(m, "ahmet@hotmail.com")
	v := m.View()
	if strings.Contains(v, "yakında") || !strings.Contains(v, "Tarayıcıda Microsoft ile giriş yap") {
		t.Fatal("Microsoft sign-in must be offered")
	}
	for _, bad := range []string{"OAuth", "IMAP", "SMTP", "token", "README"} {
		if strings.Contains(v, bad) {
			t.Fatalf("technical word %q on screen", bad)
		}
	}
	m.opt.Demo = true // demo simulates the browser
	if cmd := m.onKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil || !l.waiting {
		t.Fatal("Enter does not start Microsoft sign-in")
	}
	v = m.View()
	if !strings.Contains(v, "Tarayıcıda onay bekleniyor") || !strings.Contains(v, "k: kodla giriş") {
		t.Fatal("waiting box lacks the fallbacks")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlL})
	if len(b.copied) != 1 || !strings.HasPrefix(b.copied[0], "https://login.microsoftonline.com/common/") {
		t.Fatalf("Ctrl+L copied %v", b.copied)
	}
	// typing while waiting must not change the address
	before := l.email.Value()
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if l.email.Value() != before {
		t.Fatal("keys typed into the address while waiting")
	}
}

func TestLoginDeviceCodeScreen(t *testing.T) {
	b := stubBrowser(t, false)
	m, l := loginModelFor(t)
	typeText(m, "ahmet@hotmail.com")
	l.busy, l.waiting = true, true
	switched := false
	l.toDevice = func() { switched = true }
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if !switched {
		t.Fatal("k does not switch to the code sign-in")
	}
	m.Update(loginDeviceMsg{d: &auth.DeviceCode{URL: "https://microsoft.com/devicelogin", Code: "ABCD1234"}})
	v := m.View()
	if !strings.Contains(v, "A B C D 1 2 3 4") || !strings.Contains(v, "microsoft.com/devicelogin") {
		t.Fatal("device code not shown big with the link")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if len(b.copied) == 0 || b.copied[len(b.copied)-1] != "ABCD1234" {
		t.Fatal("c does not copy the code")
	}
	for _, sz := range sizes {
		m.applySize(sz[0], sz[1])
		checkFrame(t, "device", m)
	}
}

func TestLoginMicrosoftWithoutID(t *testing.T) {
	stubBrowser(t, false)
	old := auth.DefaultMSClientID
	auth.DefaultMSClientID = ""
	t.Cleanup(func() { auth.DefaultMSClientID = old })
	t.Setenv("TERMAIL_MS_CLIENT_ID", "")
	m, l := loginModelFor(t)
	typeText(m, "ahmet@hotmail.com")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if l.busy || l.status != msUnavailable {
		t.Fatal("a build without ID must say so plainly")
	}
}

func TestLoginKeysDoNotSwitchAccounts(t *testing.T) {
	stubBrowser(t, false)
	t.Setenv("TERMAIL_MS_CLIENT_ID", "")
	mk := func(n string) *Acct {
		return &Acct{Cfg: account.Account{Name: n, Email: n + "@x.com"}, B: backend.NewMemory(nil)}
	}
	m := New(Options{Config: &account.Config{}, Accounts: []*Acct{mk("a"), mk("b")}, ShowLogin: true})
	m.onKey(tea.KeyMsg{Type: tea.KeyF2})
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m.onKey(tea.KeyMsg{Type: tea.KeyCtrlA})
	if m.login == nil || m.cur != 0 || m.acctPicker != nil || m.login.email.Value() != "2" {
		t.Fatal("login screen keys leaked to account switching")
	}
}
