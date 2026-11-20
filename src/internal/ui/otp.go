package ui

import (
	"regexp"
	"strings"
	"unicode"

	"termail/internal/mailmsg"
)

// Only cues that sit immediately next to a code in the rendered text.
// Turkish "kod" suffixes are tried longest-first so "kodunuz" is not
// consumed as "kodu". "kody" is accepted as a typo of "kodu". Cues may
// be prefixed (onay/doğrulama/güvenlik/tek kullanımlık) and followed by
// separators with spaces (" : ", ":", "-", "is"). şifre, OTP and "code"
// are accepted only when adjacent to the value. Prefer a miss over a
// false positive (phones, dates, bare order numbers).
var otpKod = `kod(?:unuzdur|unuz|udur|larınız|larını|lar|un|um|u|y)?`
var otpAfterCue = regexp.MustCompile(`(?i)(?:(?:(?:onay|doğrulama|güvenlik|giriş|sms|tek\s+kullanımlık)\s+)` + otpKod +
	`|\b` + otpKod +
	`|şifreniz(?:dir)?|şifre(?:niz|n|si)?` +
	`|\botp\b` +
	`|(?:verification|confirmation|security|authentication)\s+code` +
	`|one[-\s]?time\s+(?:pass(?:word|code)|code)` +
	`|your\s+code(?:\s+is)?` +
	`|\bcode` +
	`)\s*(?:is\s+)?[-:：=–]?\s*([A-Z0-9]*[0-9][A-Z0-9]*(?:[- ][A-Z0-9]+){0,3})`)

var otpURL = regexp.MustCompile(`(?i)https?://\S+|www\.\S+`)

var otpDate = regexp.MustCompile(`(?i)(?:\b\d{1,2}[./]\d{1,2}[./]\d{2,4}\b|\b\d{4}-\d{2}-\d{2}\b|\b\d{1,2}-\d{1,2}-\d{4}\b)`)
var otpPrice = regexp.MustCompile(`(?i)(?:[$€£₺]|tl|try|usd|eur)\s*[\d.,]+|[\d.,]+\s*(?:[$€£₺]|tl|try|usd|eur)`)
var otpPhone = regexp.MustCompile(`(?:\+\d{1,3}[\s-]*)?(?:\(?\d{3,4}\)?[\s-]*){2,4}\d{2,4}`)

// verifyCodes finds OTP / doğrulama codes in a message, de-duplicated,
// at most three, longest/clearest first.
func verifyCodes(p *mailmsg.Parsed) []string {
	if p == nil {
		return nil
	}
	// Only the rendered body (and the subject, which the reader shows).
	// Hidden HTML and raw source are never searched.
	text := p.View
	if strings.TrimSpace(text) == "" {
		text = p.Text
	}
	return findVerifyCodes(p.Subject, text)
}

func findVerifyCodes(parts ...string) []string {
	text := otpURL.ReplaceAllString(strings.Join(parts, "\n"), " ")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(raw string) {
		raw = trimCodeCapture(raw)
		c := normalizeCode(raw)
		if c == "" || seen[c] {
			return
		}
		if looksLikeFalsePositive(text, raw, c) {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	for _, m := range otpAfterCue.FindAllStringSubmatch(text, -1) {
		add(m[1])
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func normalizeCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	c := b.String()
	if len(c) < 4 || len(c) > 10 {
		return ""
	}
	// years and ordinary 4-digit dates are not codes
	if len(c) == 4 && (c[0] == '2' && c[1] == '0' || c[0] == '1' && c[1] == '9') {
		return ""
	}
	// 7-digit all-numeric runs are almost always local phone numbers, not OTPs
	if len(c) == 7 && digitsOnly(c) {
		return ""
	}
	digits := 0
	for _, r := range c {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	if digits == 0 {
		return "" // reject words like "CODE"
	}
	if digits < 2 && len(c) < 6 {
		return ""
	}
	return c
}

func looksLikeFalsePositive(text, raw, norm string) bool {
	// skip tokens that sit inside a date, price or phone number
	for _, re := range []*regexp.Regexp{otpDate, otpPrice, otpPhone} {
		for _, m := range re.FindAllString(text, -1) {
			if strings.Contains(strings.ToUpper(m), norm) || strings.Contains(m, raw) {
				return true
			}
		}
	}
	return false
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// trimCodeCapture keeps "12 34 56" but drops a trailing word ("551299 onay").
func trimCodeCapture(s string) string {
	s = strings.TrimSpace(s)
	parts := strings.Fields(s)
	if len(parts) <= 1 {
		return s
	}
	allNum := true
	for _, p := range parts {
		for _, r := range p {
			if r != '-' && (r < '0' || r > '9') {
				allNum = false
				break
			}
		}
	}
	if allNum {
		return s
	}
	return parts[0]
}
