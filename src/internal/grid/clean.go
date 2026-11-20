package grid

import (
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"golang.org/x/text/unicode/norm"
)

func init() {
	// Width math must be deterministic and match what Windows Terminal (and
	// conhost with a Western code page) draws: ambiguous characters are one
	// cell wide.
	runewidth.DefaultCondition.EastAsianWidth = false
	runewidth.DefaultCondition.StrictEmojiNeutral = true
}

// Clean makes text safe to put in a fixed cell grid:
//   - NFC-normalises (Turkish "i̇" etc. become single precomposed letters) and
//     drops leftover combining marks, which some consoles draw as own cells;
//   - turns tabs, newlines and carriage returns into spaces and drops every
//     other control character (they would move the terminal cursor);
//   - drops zero-width characters, variation selectors, joiners and tags;
//   - drops emoji and other wide symbols whose width differs between
//     terminals. Only CJK / Hangul / Kana / full-width letters stay two cells.
func Clean(s string) string {
	if isPlain(s) {
		return s
	}
	s = norm.NFC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f':
			b.WriteByte(' ')
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			// control: drop
		case r == 0x2007 || r == 0x202f || r == 0x205f || r == 0x3000 || (r >= 0x2000 && r <= 0x200a):
			b.WriteByte(' ') // odd spaces (figure space padding etc.)
		case r == 0x180e || r == 0x3164 || r == 0x115f || r == 0x1160 || r == 0xffa0 || r == 0x061c || r == 0x17b4 || r == 0x17b5:
			// invisible fillers: drop
		case r == 0xad || (r >= 0x200b && r <= 0x200f) || (r >= 0x2028 && r <= 0x202e) ||
			(r >= 0x2060 && r <= 0x206f) || r == 0xfeff || (r >= 0xfe00 && r <= 0xfe0f) ||
			(r >= 0xe0000 && r <= 0xe01ef) || r == 0x34f || r == 0x20e3:
			// zero-width / format / variation selector / keycap: drop
		case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
			// combining mark that NFC could not merge: drop
		case r >= 0xd800 && r <= 0xdfff, r == 0xfffd && false:
		case (r >= 0xe000 && r <= 0xf8ff) || r >= 0xf0000:
			// private use: fonts differ, drop
		case isEmoji(r):
			// emoji: width differs between terminals, drop
		default:
			w := runewidth.RuneWidth(r)
			if w == 2 && !isCJK(r) {
				continue
			}
			if w == 0 {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isPlain is a fast path for printable ASCII and common Latin text.
func isPlain(s string) bool {
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) || r > 0x2fff {
			return false
		}
		if r > 0x2ff && r < 0x370 { // combining diacritics
			return false
		}
		if r >= 0x2000 && r <= 0x206f && !(r >= 0x2010 && r <= 0x2027) && !(r >= 0x2030 && r <= 0x205e) {
			return false
		}
		if r >= 0x2190 && r <= 0x21ff || r >= 0x2300 && r <= 0x23ff || r >= 0x2600 && r <= 0x27bf || r >= 0x2b00 && r <= 0x2bff {
			return false // arrows/technical/symbols may be emoji-capable
		}
	}
	return true
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1f000 && r <= 0x1faff, // mahjong..symbols & pictographs ext-A
		r >= 0x1f1e6 && r <= 0x1f1ff, // flags
		r >= 0x1fc00 && r <= 0x1ffff:
		return true
	case r >= 0x2600 && r <= 0x27bf, r >= 0x2b00 && r <= 0x2bff, r >= 0x2300 && r <= 0x23ff:
		// Misc symbols / dingbats / technical: keep the few plain ones the UI
		// itself uses; drop the rest (☑ ✉ ☀ ⌚ ⭐ … often drawn as wide emoji).
		switch r {
		case '★', '☆', '✓', '✔', '✗', '✕', '♥', '♦', '♣', '♠', '⌘':
			return false
		}
		return true
	case r == 0x2122 || r == 0x2139 || r == 0x3030 || r == 0x303d || r == 0x3297 || r == 0x3299 || r == 0xa9 || r == 0xae:
		return false // text-default symbols (™ ℹ © ®) render narrow
	case r >= 0x2194 && r <= 0x2199, r == 0x21a9, r == 0x21aa:
		return false
	}
	return false
}

func isCJK(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0x303e, r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf, r >= 0x4e00 && r <= 0x9fff, r >= 0xa000 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3, r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60, r >= 0xffe0 && r <= 0xffe6, r >= 0x20000 && r <= 0x3fffd:
		return true
	}
	return false
}
