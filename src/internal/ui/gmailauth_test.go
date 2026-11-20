package ui

import (
	"strings"
	"testing"

	"termail/internal/backend"
)

func TestCleanSecret(t *testing.T) {
	cases := []struct {
		in   string
		app  bool
		want string
	}{
		{"abcd efgh ijkl mnop", true, "abcdefghijklmnop"},
		{" abcd\u00a0efgh\tijkl\u200bmnop\r\n", true, "abcdefghijklmnop"},
		{"ab\x00cd efgh ijkl mnop\x00", true, "abcdefghijklmnop"},
		{"AbCd EfGh", true, "AbCdEfGh"}, // case never changed
		{"  benim gizli şifrem \n", false, "benim gizli şifrem"},
		{"gi\x00zli\ufeff", false, "gizli"},
	}
	for _, c := range cases {
		if got := CleanSecret(c.in, c.app); got != c.want {
			t.Errorf("CleanSecret(%q,%v) = %q, want %q", c.in, c.app, got, c.want)
		}
	}
	if gmailPassWarn("abcdefghijklmnop") != "" {
		t.Error("valid app password warned")
	}
	for _, bad := range []string{"abcdefghijklmno", "abcdefghijklmnoP", "abcdefghijklmn1p", "abcdefghijklmnopq"} {
		if gmailPassWarn(bad) == "" {
			t.Errorf("%q not warned", bad)
		}
	}
	s := passSummary("ab1 !")
	if strings.Contains(s, "ab1") || !strings.Contains(s, "uzunluk 5") || !strings.Contains(s, "harf 2") || !strings.Contains(s, "rakam 1") {
		t.Errorf("summary %q", s)
	}
}

func TestLoginRefusedShowsReasonAndRelogin(t *testing.T) {
	m, l := loginModelFor(t)
	typePlain(m, "ad@gmail.com")
	l.setFocus(1)
	typePlain(m, "abcd efgh")
	l.passWarn = gmailPassWarn(l.cleanPass())
	m.Update(loginDoneMsg{err: &backend.AuthError{Code: "AUTHENTICATIONFAILED", Text: "Invalid credentials (Failure)"}})
	if !strings.Contains(l.status, "16 küçük harf") || !strings.Contains(l.status, "Invalid credentials") {
		t.Fatalf("status %q", l.status)
	}
	// an existing account whose saved password is refused can sign in again
	m2, _ := twoAccounts(t)
	m2.accts[0].State = stNeedsLogin
	m2.login = newLogin(m2, true)
	m2.login.email.SetValue("BEN@kisisel.com ")
	m2.login.pass.SetValue("x")
	m2.login.submit()
	if strings.Contains(m2.login.status, "zaten ekli") || m2.login.relogin != 0 {
		t.Fatalf("relogin blocked: %q %d", m2.login.status, m2.login.relogin)
	}
	m2.accts[1].State = stOnline
	m2.login = newLogin(m2, true)
	m2.login.email.SetValue("ben@firma.com")
	m2.login.pass.SetValue("x")
	m2.login.submit()
	if !strings.Contains(m2.login.status, "zaten ekli") {
		t.Fatal("working account added twice")
	}
}
