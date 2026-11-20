package htmltext

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// MarkRune starts an inline link marker "[n]" in display text. It is a
// private-use character, so it never appears in real mail text (it is
// stripped from input); the reader draws it as a space and colours the
// marker that follows. Being part of the word, the marker never wraps onto
// a line of its own.
const MarkRune = '\uE000'

// Link is one distinct link found in a mail.
type Link struct {
	URL  string // full destination (http, https or mailto)
	Text string // what the mail shows for it (anchor text or the address)
}

// Links collects the distinct links of one mail and numbers them 1, 2, ...
type Links struct {
	List  []Link
	index map[string]int
}

// Add records a link and returns its number (the same number for the same
// address), or 0 when the link is not one we offer (unsafe scheme, empty).
func (s *Links) Add(raw, text string) int {
	u, ok := Normalize(raw)
	if !ok {
		return 0
	}
	if s.index == nil {
		s.index = map[string]int{}
	}
	k := strings.TrimSuffix(u, "/")
	if n, ok := s.index[k]; ok {
		if s.List[n-1].Text == "" && text != "" {
			s.List[n-1].Text = text
		}
		return n
	}
	text = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r == MarkRune || Invisible(r) {
			return -1
		}
		return r
	}, text)), " ")
	s.List = append(s.List, Link{URL: u, Text: text})
	s.index[k] = len(s.List)
	return len(s.List)
}

// Marker is the inline marker for link n.
func Marker(n int) string { return string(MarkRune) + "[" + strconv.Itoa(n) + "]" }

// Normalize checks a link and returns it in a canonical form. Only http,
// https and mailto are accepted ("www.x" becomes "https://www.x"); anything
// else (javascript:, data:, cid:, file:, ...) is refused. The result never
// contains spaces, quotes or control characters.
func Normalize(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(raw), "www.") {
		raw = "https://" + raw
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || r == MarkRune || Invisible(r) {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" || u.User != nil || strings.ContainsAny(u.Host, `"'\ `) {
			return "", false
		}
		host, err := idna.Lookup.ToASCII(strings.ToLower(u.Hostname()))
		if err != nil || host == "" {
			return "", false
		}
		if port := u.Port(); port != "" {
			host += ":" + port
		}
		u.Host = host
	case "mailto":
		if u.Opaque == "" || !strings.Contains(u.Opaque, "@") {
			return "", false
		}
	default:
		return "", false
	}
	out := u.String()
	if strings.ContainsAny(out, " \"'`\\<>") {
		return "", false
	}
	return out, true
}

// Domain is the destination shown to the user: the host of a web link
// (without "www.") or the mail domain of a mailto link.
func Domain(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	if p.Scheme == "mailto" {
		addr := p.Opaque
		if i := strings.IndexByte(addr, '?'); i >= 0 {
			addr = addr[:i]
		}
		if i := strings.LastIndexByte(addr, '@'); i >= 0 {
			return strings.ToLower(addr[i+1:])
		}
		return ""
	}
	h := strings.ToLower(p.Hostname())
	if uh, err := idna.ToUnicode(h); err == nil {
		h = uh
	}
	return strings.TrimPrefix(h, "www.")
}

// Shorten gives a short display form of a long link: domain, the start of
// the path, then "…". Links up to max characters are returned as written.
func Shorten(u string, max int) string {
	if utf8.RuneCountInString(u) <= max {
		return u
	}
	p, err := url.Parse(u)
	if err != nil || p.Scheme == "mailto" {
		return truncRunes(u, max-1) + "…"
	}
	host := strings.TrimPrefix(p.Host, "www.")
	rest := p.EscapedPath()
	if p.RawQuery != "" {
		rest += "?" + p.RawQuery
	}
	room := max - utf8.RuneCountInString(host) - 1
	if room < 4 {
		return truncRunes(host, max-1) + "…"
	}
	return host + truncRunes(rest, room) + "…"
}

func truncRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}

// ShortMax is the longest link shown in full in the reader.
const ShortMax = 48

var urlRe = regexp.MustCompile(`(?i)\b(?:https?://|mailto:|www\.)[^\s<>"'\x{00A0}\x{E000}` + "\u201c\u201d\u00ab\u00bb" + `]+`)

// trimURL cuts trailing punctuation that belongs to the sentence, and a
// closing bracket that has no opening partner inside the link.
func trimURL(s string) string {
	for s != "" {
		r, sz := utf8.DecodeLastRuneInString(s)
		switch r {
		case '.', ',', ';', ':', '!', '?', '\'', '"', '…', '’', '”', '»', '*', '_':
			s = s[:len(s)-sz]
			continue
		case ')':
			if strings.Count(s, "(") < strings.Count(s, ")") {
				s = s[:len(s)-sz]
				continue
			}
		case ']':
			if strings.Count(s, "[") < strings.Count(s, "]") {
				s = s[:len(s)-sz]
				continue
			}
		case '>':
			s = s[:len(s)-sz]
			continue
		}
		break
	}
	return s
}

// Linkify finds written links in plain text, numbers them in set and puts a
// marker after each one; long links are replaced by a short display form.
// Links already followed by a marker (anchor text that is itself the link)
// are left alone.
func Linkify(text string, set *Links) string {
	locs := urlRe.FindAllStringIndex(text, -1)
	if locs == nil {
		return text
	}
	var b strings.Builder
	last := 0
	for _, l := range locs {
		start, end := l[0], l[1]
		if start < last {
			continue
		}
		raw := trimURL(text[start:end])
		end = start + len(raw)
		if strings.HasPrefix(strings.TrimLeft(text[end:], " "), string(MarkRune)) {
			continue // already numbered (HTML anchor)
		}
		low := strings.ToLower(raw)
		if low == "www." || low == "mailto:" || low == "http://" || low == "https://" {
			continue
		}
		n := set.Add(raw, "")
		if n == 0 {
			continue
		}
		b.WriteString(text[last:start])
		disp := raw
		if strings.HasPrefix(low, "mailto:") {
			disp = raw[len("mailto:"):]
		}
		disp = Shorten(disp, ShortMax)
		if set.List[n-1].Text == "" {
			set.List[n-1].Text = disp
		}
		b.WriteString(disp + Marker(n))
		last = end
	}
	b.WriteString(text[last:])
	return b.String()
}

var domainRe = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?((?:[a-z0-9\p{L}](?:[a-z0-9\p{L}-]*[a-z0-9\p{L}])?\.)+[a-z\p{L}]{2,})`)

// Mismatch reports whether the visible text of a link names a web address
// that is not where the link really goes (a classic phishing trick):
// "bankam.com.tr" pointing to "bankam-giris.xyz". Only text that is itself
// written like an address counts; ordinary words never warn.
func Mismatch(text, u string) bool {
	dest := Domain(u)
	if dest == "" || strings.HasPrefix(u, "mailto:") {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.TrimRight(t, ".,;:!?)]")
	t = strings.TrimLeft(t, "([")
	if t == "" || strings.ContainsAny(t, " \t") {
		return false
	}
	if strings.Contains(t, "@") && !strings.Contains(t, "/") {
		return false // an e-mail address, not a web address
	}
	m := domainRe.FindStringSubmatchIndex(t)
	if m == nil || m[0] != 0 {
		return false
	}
	shown := strings.TrimPrefix(t[m[2]:m[3]], "www.")
	rest := t[m[1]:]
	if rest != "" && rest[0] != '/' && rest[0] != ':' && rest[0] != '?' {
		return false
	}
	explicit := strings.Contains(t, "://") || strings.HasPrefix(t, "www.")
	if !explicit && !knownTLD(shown) {
		return false // "fatura.pdf", "v1.2" ...
	}
	return !sameSite(shown, dest)
}

func sameSite(a, b string) bool {
	return a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

// knownTLD keeps file names like "fatura.pdf" from counting as addresses
// when the text has no "http" or "www." in front.
func knownTLD(d string) bool {
	i := strings.LastIndexByte(d, '.')
	if i < 0 {
		return false
	}
	switch d[i+1:] {
	case "com", "net", "org", "tr", "info", "biz", "io", "co", "me", "app", "dev", "xyz", "online", "site",
		"shop", "store", "top", "club", "live", "link", "click", "de", "uk", "us", "eu", "ru", "cn", "fr",
		"nl", "it", "es", "gov", "edu", "ly", "to", "gl", "be", "ch", "at", "az", "tk", "ml", "ga", "cf", "gq":
		return true
	}
	return false
}
