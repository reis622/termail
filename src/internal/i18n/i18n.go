// Package i18n holds Termail's Turkish and English UI strings.
// The default language is Turkish so existing tests keep passing.
// Language is switched with Set; T/Tf look up the current catalog.
package i18n

import (
	"fmt"
	"strings"
	"sync"
)

// Lang is a UI language code ("tr" or "en").
type Lang string

const (
	TR Lang = "tr"
	EN Lang = "en"
)

var (
	mu  sync.RWMutex
	cur = TR
)

// Set changes the current UI language. Unknown values fall back to Turkish.
func Set(l Lang) {
	if l != EN && l != TR {
		l = TR
	}
	mu.Lock()
	cur = l
	mu.Unlock()
}

// Get returns the current UI language.
func Get() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return cur
}

// Parse maps a config / user value to a language. ok is false if s is empty
// or not recognised.
func Parse(s string) (Lang, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tr", "tr-tr", "tr_tr", "turkish", "turkce", "türkce", "türkçe":
		return TR, true
	case "en", "en-us", "en_us", "en-gb", "en_gb", "english", "ingilizce":
		return EN, true
	}
	return "", false
}

// Resolve uses a config value when it is a known language, otherwise Detect.
func Resolve(cfg string) Lang {
	if l, ok := Parse(cfg); ok {
		return l
	}
	return Detect()
}

// T returns the translation of the Turkish source string in the current
// language. Missing English entries fall back to the Turkish text.
func T(tr string) string {
	if Get() != EN {
		return tr
	}
	if en, ok := enUS[tr]; ok {
		return en
	}
	return tr
}

// Tf is T followed by fmt.Sprintf.
func Tf(tr string, args ...any) string {
	return fmt.Sprintf(T(tr), args...)
}

// Error translates err.Error() when it is a known UI string.
func Error(err error) string {
	if err == nil {
		return ""
	}
	return T(err.Error())
}

// FilterHints are the chips under an empty search bar, in the current language.
func FilterHints() []string {
	if Get() == EN {
		return []string{"from:", "has:attachment", "unread", "saved", "today", "this week"}
	}
	return []string{"kimden:", "ekli", "okunmamış", "kaydedilen", "bugün", "bu hafta"}
}
