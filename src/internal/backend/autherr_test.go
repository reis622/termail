package backend

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAuthReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&AuthError{Code: "AUTHENTICATIONFAILED", Text: "Invalid credentials (Failure)"}, "kabul etmedi"},
		{&AuthError{Code: "ALERT", Text: "Application-specific password required: https://support.google.com/accounts/answer/185833 (Failure)"}, "uygulama şifresi istiyor"},
		{&AuthError{Code: "WEBALERT", Text: "Web login required: https://accounts.google.com/signin/continue"}, "tarayıcıdan giriş"},
		{&AuthError{Code: "ALERT", Text: "Your account is not enabled for IMAP use. Please visit your Gmail settings page and enable your account for IMAP access."}, "IMAP erişimi kapalı"},
		{&AuthError{Code: "ALERT", Text: "Too many simultaneous connections. (Failure)"}, "Çok fazla"},
		{&AuthError{Code: "ALERT", Text: "Bakım çalışması"}, "uyarı gönderdi"},
		{fmt.Errorf("%w: 535-5.7.8 Username and Password not accepted", ErrAuth), "kabul etmedi"},
	}
	for _, c := range cases {
		got := AuthReason(c.err)
		if !errors.Is(c.err, ErrAuth) || !strings.Contains(got, c.want) || !strings.Contains(got, "Sunucu:") {
			t.Errorf("%v → %q (want %q)", c.err, got, c.want)
		}
	}
	if AuthReason(errors.New("timeout")) != "" {
		t.Error("non-auth error mapped")
	}
	long := AuthReason(&AuthError{Text: strings.Repeat("x", 300) + "\x00\n"})
	if strings.ContainsRune(long, 0) || len([]rune(long)) > 220 {
		t.Errorf("server text not shortened: %d", len(long))
	}
}
