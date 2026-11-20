// Package search parses what the user types into the search bar and
// matches mail headers / text against it. Matching is Turkish-aware and
// ignores case and accents: "ayse", "AYŞE" and "Ayşe" are the same, and so
// are I / ı / İ / i.
//
// Filters are plain Turkish words: "kimden:ayşe", "ekli", "okunmamış",
// "kaydedilen", "bugün", "dün", "bu hafta", "bu ay". Everything else is a
// word that must appear somewhere (sender, subject or text).
package search

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"termail/internal/i18n"
)

// DateKind is a date filter.
type DateKind int

const (
	DateAny DateKind = iota
	DateToday
	DateYesterday
	DateThisWeek
	DateThisMonth
)

// Query is a parsed search.
type Query struct {
	Raw    string
	Words  []string // as typed (for the server and for highlighting)
	From   []string // kimden:/from: values
	To     []string // kime:/to: values
	Attach bool     // ekli / has:attachment
	Unread bool     // okunmamış / unread
	Saved  bool     // kaydedilen / saved
	Date   DateKind

	words, from, to []string // folded
}

// Empty reports whether there is nothing to search for.
func (q Query) Empty() bool {
	return len(q.Words) == 0 && len(q.From) == 0 && len(q.To) == 0 && !q.Attach && !q.Unread && !q.Saved && q.Date == DateAny
}

// HasFilters reports whether any filter (not a plain word) is set.
func (q Query) HasFilters() bool {
	return len(q.From) > 0 || len(q.To) > 0 || q.Attach || q.Unread || q.Saved || q.Date != DateAny
}

// Needles are the strings to highlight.
func (q Query) Needles() []string {
	return append(append(append([]string(nil), q.Words...), q.From...), q.To...)
}

// Chips are the filters as short labels ("Kimden: Ayşe", "Ekli", ...).
func (q Query) Chips() []string {
	var out []string
	for _, f := range q.From {
		out = append(out, i18n.T("Kimden: ")+upperFirst(f))
	}
	for _, f := range q.To {
		out = append(out, i18n.T("Kime: ")+upperFirst(f))
	}
	if q.Attach {
		out = append(out, i18n.T("Ekli"))
	}
	if q.Unread {
		out = append(out, i18n.T("Okunmamış"))
	}
	if q.Saved {
		out = append(out, i18n.T("Kaydedilen"))
	}
	switch q.Date {
	case DateToday:
		out = append(out, i18n.T("Bugün"))
	case DateYesterday:
		out = append(out, i18n.T("Dün"))
	case DateThisWeek:
		out = append(out, i18n.T("Bu hafta"))
	case DateThisMonth:
		out = append(out, i18n.T("Bu ay"))
	}
	return out
}

// FilterHints are the filter words offered under an empty search bar,
// in the current UI language. Both Turkish and English forms are always accepted by Parse.
func FilterHints() []string { return i18n.FilterHints() }

// tokens splits on spaces, keeping "quoted phrases" together.
func tokens(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			if quoted {
				flush()
			}
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

// Parse reads the text of the search bar.
func Parse(s string) Query {
	q := Query{Raw: s}
	t := tokens(s)
	for i := 0; i < len(t); i++ {
		w := t[i]
		f := Fold(w)
		switch {
		case strings.HasPrefix(f, "kimden:") || strings.HasPrefix(f, "from:"):
			v := w[strings.IndexByte(w, ':')+1:]
			if v == "" && i+1 < len(t) && !isFilterWord(Fold(t[i+1])) {
				i++
				v = t[i]
			}
			if v != "" {
				q.From = append(q.From, v)
			}
			continue
		case strings.HasPrefix(f, "kime:") || strings.HasPrefix(f, "to:"):
			v := w[strings.IndexByte(w, ':')+1:]
			if v == "" && i+1 < len(t) && !isFilterWord(Fold(t[i+1])) {
				i++
				v = t[i]
			}
			if v != "" {
				q.To = append(q.To, v)
			}
			continue
		case f == "ekli" || f == "ek:var" || f == "ekler" || f == "attachment" || f == "attachments" ||
			f == "has:attachment" || f == "has:attach" || f == "has:ek":
			q.Attach = true
			continue
		case f == "okunmamis" || f == "okunmamislar" || f == "unread" || f == "unseen":
			q.Unread = true
			continue
		case f == "kaydedilen" || f == "kaydedilenler" || f == "yildizli" || f == "saved" || f == "starred" || f == "flagged":
			q.Saved = true
			continue
		case f == "bugun" || f == "today":
			q.Date = DateToday
			continue
		case f == "dun" || f == "yesterday":
			q.Date = DateYesterday
			continue
		case (f == "bu" || f == "this") && i+1 < len(t):
			switch Fold(t[i+1]) {
			case "hafta", "week":
				q.Date, i = DateThisWeek, i+1
				continue
			case "ay", "month":
				q.Date, i = DateThisMonth, i+1
				continue
			}
		}
		q.Words = append(q.Words, w)
	}
	for _, w := range q.Words {
		q.words = append(q.words, Fold(w))
	}
	for _, w := range q.From {
		q.from = append(q.from, Fold(w))
	}
	for _, w := range q.To {
		q.to = append(q.to, Fold(w))
	}
	return q
}

func isFilterWord(f string) bool {
	switch f {
	case "ekli", "okunmamis", "kaydedilen", "bugun", "dun", "bu",
		"unread", "unseen", "saved", "starred", "flagged", "today", "yesterday", "this",
		"attachment", "attachments":
		return true
	}
	return strings.Contains(f, ":")
}

// Range is the time window of the date filter (zero times = no limit).
func (q Query) Range(now time.Time) (since, before time.Time) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch q.Date {
	case DateToday:
		return day, time.Time{}
	case DateYesterday:
		return day.AddDate(0, 0, -1), day
	case DateThisWeek: // weeks start on Monday
		wd := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -wd), time.Time{}
	case DateThisMonth:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), time.Time{}
	}
	return time.Time{}, time.Time{}
}

// Item is what a mail offers for matching. Hay is the folded sender,
// address and subject (see Fold); Body is the folded text ("" if unknown).
type Item struct {
	FromHay string // folded sender name + address
	ToHay   string // folded To/Cc names and addresses
	Hay     string // folded sender + address + subject
	Body    string // folded text, may be empty
	Date    time.Time
	Seen    bool
	Flagged bool
	Attach  bool
}

// Tier ranks a match: 0 when a searched word (or kimden:) is in the
// sender name / address, 1 when it is in the subject, 2 when only the
// text matched. A query of filters only puts everything in tier 0.
func (q Query) Tier(it Item) int {
	if len(q.words) == 0 {
		return 0
	}
	for _, w := range q.words {
		if strings.Contains(it.FromHay, w) {
			return 0
		}
	}
	subj := it.Hay
	if strings.HasPrefix(subj, it.FromHay) {
		subj = subj[len(it.FromHay):]
	}
	for _, w := range q.words {
		if strings.Contains(subj, w) {
			return 1
		}
	}
	return 2
}

// Match reports whether the item fits every part of the query.
func (q Query) Match(it Item, now time.Time) bool {
	if q.Attach && !it.Attach || q.Unread && it.Seen || q.Saved && !it.Flagged {
		return false
	}
	if q.Date != DateAny {
		since, before := q.Range(now)
		if it.Date.Before(since) || !before.IsZero() && !it.Date.Before(before) {
			return false
		}
	}
	for _, f := range q.from {
		if !strings.Contains(it.FromHay, f) {
			return false
		}
	}
	for _, f := range q.to {
		if !strings.Contains(it.ToHay, f) {
			return false
		}
	}
	for _, w := range q.words {
		if !strings.Contains(it.Hay, w) && !strings.Contains(it.Body, w) {
			return false
		}
	}
	return true
}

// foldRune maps one rune to its search form: lower case, Turkish dotted /
// dotless i as plain i, accents removed. Exactly one rune comes out for
// each rune that goes in, so positions stay the same.
func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			return r + 32
		}
		return r
	}
	switch r {
	case 'İ', 'I', 'ı', 'î', 'Î', 'ì', 'í', 'Ì', 'Í', 'ï', 'Ï':
		return 'i'
	case 'ş', 'Ş':
		return 's'
	case 'ğ', 'Ğ':
		return 'g'
	case 'ü', 'Ü', 'û', 'Û':
		return 'u'
	case 'ö', 'Ö':
		return 'o'
	case 'ç', 'Ç':
		return 'c'
	case 'â', 'Â':
		return 'a'
	}
	r = unicode.ToLower(r)
	if r >= 0xC0 && r < 0x250 { // Latin letters with accents
		d := norm.NFD.String(string(r))
		if f, _ := utf8.DecodeRuneInString(d); f != utf8.RuneError {
			return unicode.ToLower(f)
		}
	}
	return r
}

// Fold returns the search form of s (see foldRune).
func Fold(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= utf8.RuneSelf || 'A' <= c && c <= 'Z' {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(foldRune(r))
	}
	return b.String()
}

// Spans returns the rune ranges [start, end) of text where any needle
// matches (folded). Overlapping matches are merged.
func Spans(text string, needles []string) [][2]int {
	if len(needles) == 0 || text == "" {
		return nil
	}
	tr := []rune(text)
	fr := make([]rune, len(tr))
	for i, r := range tr {
		fr[i] = foldRune(r)
	}
	mark := make([]bool, len(fr))
	any := false
	for _, n := range needles {
		nr := []rune(Fold(n))
		if len(nr) == 0 || len(nr) > len(fr) {
			continue
		}
		for i := 0; i+len(nr) <= len(fr); i++ {
			if fr[i] != nr[0] {
				continue
			}
			ok := true
			for j := 1; j < len(nr); j++ {
				if fr[i+j] != nr[j] {
					ok = false
					break
				}
			}
			if ok {
				for j := range nr {
					mark[i+j] = true
				}
				any = true
			}
		}
	}
	if !any {
		return nil
	}
	var out [][2]int
	for i := 0; i < len(mark); {
		if !mark[i] {
			i++
			continue
		}
		j := i
		for j < len(mark) && mark[j] {
			j++
		}
		out = append(out, [2]int{i, j})
		i = j
	}
	return out
}

// Snippet returns about width runes of text around the first match,
// on one line, with "…" where it was cut ("" when nothing matches).
func Snippet(text string, needles []string, width int) string {
	if width < 8 {
		return ""
	}
	flat := strings.Join(strings.Fields(text), " ")
	sp := Spans(flat, needles)
	if len(sp) == 0 {
		return ""
	}
	r := []rune(flat)
	start := sp[0][0] - width/4
	if start < 0 {
		start = 0
	}
	// begin at a word boundary
	if start > 0 {
		for k := start; k < sp[0][0]; k++ {
			if r[k-1] == ' ' {
				start = k
				break
			}
		}
	}
	end := min(len(r), start+width-1)
	s := string(r[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(r) && start == 0 {
		s += "…"
	}
	return s
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	switch r {
	case 'i':
		return "İ" + s[n:]
	case 'ı':
		return "I" + s[n:]
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// UpperFirst capitalises the first letter, the Turkish way (i -> İ).
func UpperFirst(s string) string { return upperFirst(s) }
