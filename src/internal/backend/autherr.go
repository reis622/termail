package backend

import (
	"errors"
	"strings"
	"unicode"
)

// AuthError is a refused sign-in with the server's own code and text.
// errors.Is(err, ErrAuth) holds for it.
type AuthError struct {
	Code string // e.g. AUTHENTICATIONFAILED, ALERT, WEBALERT
	Text string // the server's short text, e.g. "Invalid credentials (Failure)"
}

func (e *AuthError) Error() string {
	s := ErrAuth.Error()
	if e.Code != "" {
		s += ": [" + e.Code + "]"
	}
	if e.Text != "" {
		s += " " + e.Text
	}
	return s
}

func (e *AuthError) Is(t error) bool { return t == ErrAuth }

// AuthReason explains a refused sign-in in plain Turkish, followed by the
// server's own short text. "" when err is not a refused sign-in.
func AuthReason(err error) string {
	if !errors.Is(err, ErrAuth) {
		return ""
	}
	code, text := "", err.Error()
	var ae *AuthError
	if errors.As(err, &ae) {
		code, text = strings.ToUpper(ae.Code), ae.Text
	}
	low := strings.ToLower(text)
	var why string
	switch {
	case strings.Contains(low, "application-specific password required"):
		why = "Bu hesap uygulama şifresi istiyor; normal hesap şifren burada çalışmaz. Ctrl + O harfi ile uygulama şifresi al ve onu yapıştır."
	case code == "WEBALERT" || strings.Contains(low, "web login required") || strings.Contains(low, "log in via your web browser"):
		why = "Sağlayıcı önce tarayıcıdan giriş yapmanı istiyor. Tarayıcıda hesabına bir kez gir, güvenlik uyarısını onayla, sonra tekrar dene."
	case strings.Contains(low, "imap") && (strings.Contains(low, "disabled") || strings.Contains(low, "not enabled") || strings.Contains(low, "enable imap")):
		why = "Hesabında IMAP erişimi kapalı. Gmail'de: Ayarlar → Yönlendirme ve POP/IMAP → IMAP'i etkinleştir."
	case strings.Contains(low, "too many") || strings.Contains(low, "rate limit") || strings.Contains(low, "try again later"):
		why = "Çok fazla deneme yapıldı. Birkaç dakika bekleyip tekrar dene."
	case code == "AUTHENTICATIONFAILED" || strings.Contains(low, "invalid credentials") || strings.Contains(low, "authentication failed") || strings.Contains(low, "not accepted"):
		why = "Sunucu adresi ya da şifreyi kabul etmedi. Uygulama şifresi kullanıyorsan yeni bir tane oluşturup boşluksuz olarak yapıştır."
	case code == "ALERT":
		why = "Sunucu bir uyarı gönderdi."
	default:
		why = "Giriş reddedildi; e-posta ya da şifre hatalı."
	}
	if t := shortServerText(text); t != "" {
		why += " (Sunucu: \"" + t + "\")"
	}
	return why
}

// shortServerText: the server's text, single line, printable, at most 100 chars.
func shortServerText(s string) string {
	s = strings.TrimPrefix(s, ErrAuth.Error()+": ")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsPrint(r) {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteByte(' ')
		}
	}
	t := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(t); len(r) > 100 {
		t = string(r[:100]) + "…"
	}
	return t
}
