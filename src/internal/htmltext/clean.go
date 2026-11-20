package htmltext

import (
	"html"
	"strings"
)

// CleanText makes mail text readable: it decodes HTML entities that slipped
// through as literal text (also double-escaped ones like "&amp;#847;", which
// marketing mails use as invisible "preheader" padding), removes invisible
// and zero-width characters, collapses runs of spaces and drops lines that
// only contained padding. Paragraph breaks are kept (at most one blank line).
func CleanText(s string) string {
	if strings.IndexByte(s, '&') >= 0 {
		for i := 0; i < 3 && strings.IndexByte(s, '&') >= 0; i++ {
			d := html.UnescapeString(s)
			if d == s {
				break
			}
			s = d
		}
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		was := strings.TrimSpace(l) != ""
		l = cleanLine(l)
		if l == "" {
			if was {
				continue // a line of pure padding disappears completely
			}
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// Invisible reports characters that take no visible space (or only pad).
func Invisible(r rune) bool {
	switch {
	case r == 0x00ad, r == 0x034f, r == 0x061c, r == 0x115f, r == 0x1160, r == 0x17b4, r == 0x17b5,
		r == 0x180e, r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e,
		r >= 0x2060 && r <= 0x206f, r == 0x3164, r == 0xfeff, r == 0xffa0,
		r >= 0xfe00 && r <= 0xfe0f, r >= 0xe0000 && r <= 0xe01ef:
		return true
	}
	return false
}

// cleanLine drops invisible characters, turns odd spaces into plain ones,
// collapses space runs (keeping leading indentation for quotes/code) and
// trims the end.
func cleanLine(l string) string {
	padded := false // the line had invisible / odd space characters
	for _, r := range l {
		if Invisible(r) || r == 0x2007 || r == 0x2000 || r == 0x3000 {
			padded = true
			break
		}
	}
	var b strings.Builder
	b.Grow(len(l))
	lead := true
	spaces := 0
	for _, r := range l {
		if Invisible(r) {
			continue
		}
		switch r {
		case '\t':
			if lead {
				b.WriteString("    ")
				continue
			}
			r = ' '
		case 0x00a0, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a, 0x202f, 0x205f, 0x3000:
			r = ' '
		}
		if r == ' ' {
			if lead {
				if !padded {
					b.WriteByte(' ')
				}
				continue
			}
			spaces++
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		if spaces > 0 {
			if padded {
				spaces = 1 // padding collapses to one space
			}
			b.WriteString(strings.Repeat(" ", spaces))
			spaces = 0
		}
		lead = false
		b.WriteRune(r)
	}
	res := b.String()
	if strings.TrimSpace(res) == "" {
		return ""
	}
	return res
}

// hiddenStyle reports inline styles used to hide preheader text.
func hiddenStyle(style string) bool {
	s := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(style, " ", ""), "\t", ""))
	if s == "" {
		return false
	}
	for _, p := range []string{"display:none", "mso-hide:all", "visibility:hidden", "max-height:0;", "max-height:0px", "opacity:0;", "font-size:0;", "font-size:0px", "line-height:0;"} {
		if strings.Contains(s+";", p) || strings.Contains(s, p) {
			return true
		}
	}
	return strings.HasSuffix(s, "max-height:0") || strings.HasSuffix(s, "opacity:0") || strings.HasSuffix(s, "font-size:0")
}
