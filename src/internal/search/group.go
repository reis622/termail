package search

import "strings"

// Grouping by sender: mail from the same organisation (registrable domain,
// e.g. accounts.google.com and google.com -> google.com) belongs together.
// Free mail providers (gmail.com, outlook.com, ...) are shared by many
// people, so there the full address is the group.

// twoLevel are public suffixes with two labels (a small list that covers
// the usual cases; a domain under one of these keeps three labels).
var twoLevel = map[string]bool{}

func init() {
	for _, tld := range []string{"tr", "uk", "au", "jp", "kr", "br", "in", "za", "nz", "il", "mx", "ar", "cn", "tw", "hk", "sg", "my", "id", "th", "ua", "ru", "pl", "cy", "az", "eg", "sa", "ae", "pk"} {
		for _, sld := range []string{"com", "co", "org", "net", "gov", "edu", "ac", "gen", "bel", "k12", "av", "web", "biz", "info", "tv", "tsk", "pol", "mil", "dr", "bbs", "name", "tel", "ne", "or", "go", "ltd", "plc", "me", "nhs", "sch"} {
			twoLevel[sld+"."+tld] = true
		}
	}
}

// freeMail are providers where every address is a different person.
var freeMail = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "hotmail.com": true, "hotmail.com.tr": true, "outlook.com": true,
	"outlook.com.tr": true, "live.com": true, "msn.com": true, "yahoo.com": true, "yahoo.com.tr": true,
	"icloud.com": true, "me.com": true, "mac.com": true, "yandex.com": true, "yandex.com.tr": true, "yandex.ru": true,
	"mynet.com": true, "aol.com": true, "proton.me": true, "protonmail.com": true, "gmx.com": true, "gmx.de": true,
	"mail.ru": true, "zoho.com": true, "windowslive.com": true, "superonline.com": true, "ttmail.com": true,
}

// Registrable returns the registrable part of a domain ("eTLD+1"):
// accounts.google.com -> google.com, fatura.turkcell.com.tr -> turkcell.com.tr.
func Registrable(domain string) string {
	d := strings.Trim(strings.ToLower(domain), ". ")
	parts := strings.Split(d, ".")
	if len(parts) <= 2 {
		return d
	}
	n := 2
	if twoLevel[parts[len(parts)-2]+"."+parts[len(parts)-1]] {
		n = 3
	}
	return strings.Join(parts[len(parts)-n:], ".")
}

// GroupKey is the group a sender belongs to ("d:google.com" or, for free
// mail providers and odd addresses, "a:" + the address).
func GroupKey(addr string) string {
	a := strings.ToLower(strings.TrimSpace(addr))
	at := strings.LastIndexByte(a, '@')
	if at < 0 || at == len(a)-1 {
		return "a:" + a
	}
	reg := Registrable(a[at+1:])
	if freeMail[reg] || freeMail[a[at+1:]] {
		return "a:" + a
	}
	return "d:" + reg
}

// GroupLabel names a group from its senders' display names: the words they
// all start with ("Google Play", "Google" -> "Google"), else the name of
// the domain ("trendyol.com" -> "Trendyol").
func GroupLabel(key string, names []string) string {
	var common []string
	for i, n := range names {
		w := strings.Fields(n)
		if i == 0 {
			common = w
			continue
		}
		k := 0
		for k < len(common) && k < len(w) && Fold(common[k]) == Fold(w[k]) {
			k++
		}
		common = common[:k]
		if k == 0 {
			break
		}
	}
	if len(common) > 0 {
		return strings.Join(common, " ")
	}
	if strings.HasPrefix(key, "d:") {
		d := key[2:]
		if i := strings.IndexByte(d, '.'); i > 0 {
			d = d[:i]
		}
		return upperFirst(d)
	}
	if len(names) > 0 && names[0] != "" {
		return names[0]
	}
	return strings.TrimPrefix(key, "a:")
}
