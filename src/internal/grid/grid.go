// Package grid is a tiny cell buffer: the UI draws text, boxes and fills
// into it (like a character grid) and renders it once per frame. This makes
// pixel-exact layouts and overlays (toasts, dialogs) simple.
package grid

import (
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// Cell is one terminal cell.
type Cell struct {
	Ch   string
	FG   string
	BG   string
	Bold bool
	cont bool // right half of a wide character
	wide bool // Ch is two cells wide
}

// Grid is a W x H cell buffer.
type Grid struct {
	W, H int
	c    [][]Cell
	FG   string
}

// New creates a grid filled with bg.
func New(w, h int, fg, bg string) *Grid {
	g := &Grid{W: w, H: h, FG: fg}
	g.c = make([][]Cell, h)
	for y := range g.c {
		row := make([]Cell, w)
		for x := range row {
			row[x] = Cell{Ch: " ", FG: fg, BG: bg}
		}
		g.c[y] = row
	}
	return g
}

// Reuse returns g cleared to bg when it has the same size, else a new grid
// (saves allocating a fresh buffer every frame).
func Reuse(g *Grid, w, h int, fg, bg string) *Grid {
	if g == nil || g.W != w || g.H != h {
		return New(w, h, fg, bg)
	}
	g.FG = fg
	blank := Cell{Ch: " ", FG: fg, BG: bg}
	for _, row := range g.c {
		for x := range row {
			row[x] = blank
		}
	}
	return g
}

// PutClean is Put for text that is already cleaned and truncated.
func (g *Grid) PutClean(x, y int, text string, st Style) int {
	return g.put(x, y, text, st)
}

// At returns a pointer to a cell (nil when outside).
func (g *Grid) At(x, y int) *Cell {
	if x < 0 || y < 0 || x >= g.W || y >= g.H {
		return nil
	}
	return &g.c[y][x]
}

// Fill paints a rectangle with bg (clearing text).
func (g *Grid) Fill(x, y, w, h int, bg string) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if c := g.At(xx, yy); c != nil {
				*c = Cell{Ch: " ", FG: g.FG, BG: bg}
			}
		}
	}
}

// Width is the display width of s.
func Width(s string) int { return runewidth.StringWidth(Clean(s)) }

// Truncate cuts s to at most w cells, ending with "…" when cut.
func Truncate(s string, w int) string {
	s = Clean(s)
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

// TruncateLeft keeps the end of s ("…ads\fatura.pdf").
func TruncateLeft(s string, w int) string {
	s = Clean(s)
	if runewidth.StringWidth(s) <= w {
		return s
	}
	r := []rune(s)
	for i := range r {
		if runewidth.StringWidth(string(r[i:])) <= w-1 {
			return "…" + string(r[i:])
		}
	}
	return "…"
}

// Style for Put.
type Style struct {
	FG, BG string // empty BG keeps the existing background
	Bold   bool
}

// Put writes text at x,y; maxw > 0 truncates with "…". Returns the x after the text.
func (g *Grid) Put(x, y int, text string, st Style, maxw int) int {
	if maxw > 0 {
		text = Truncate(text, maxw)
	} else {
		text = Clean(text)
	}
	return g.put(x, y, text, st)
}

func (g *Grid) put(x, y int, text string, st Style) int {
	fg := st.FG
	if fg == "" {
		fg = g.FG
	}
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if w == 0 { // Clean removed these; never glue invisible runes into a cell
			continue
		}
		if x < 0 {
			x += w
			continue
		}
		if x+w > g.W {
			return x
		}
		if c := g.At(x, y); c != nil {
			c.Ch, c.FG, c.Bold, c.cont, c.wide = runeStr(r), fg, st.Bold, false, w == 2
			if st.BG != "" {
				c.BG = st.BG
			}
			if w == 2 {
				if c2 := g.At(x+1, y); c2 != nil {
					c2.Ch, c2.cont, c2.wide = "", true, false
					if st.BG != "" {
						c2.BG = st.BG
					}
				}
			}
		}
		x += w
	}
	return x
}

// RPut writes text so that it ends right before xr.
func (g *Grid) RPut(xr, y int, text string, st Style) int {
	return g.Put(xr-Width(text), y, text, st, 0)
}

// Box draws a rounded border with an optional title.
func (g *Grid) Box(x, y, w, h int, fg, bg, title, tfg string) {
	if w < 2 || h < 2 {
		return
	}
	if bg != "" {
		g.Fill(x, y, w, h, bg)
	}
	s := Style{FG: fg}
	g.Put(x, y, "╭"+strings.Repeat("─", w-2)+"╮", s, 0)
	g.Put(x, y+h-1, "╰"+strings.Repeat("─", w-2)+"╯", s, 0)
	for yy := y + 1; yy < y+h-1; yy++ {
		g.Put(x, yy, "│", s, 0)
		g.Put(x+w-1, yy, "│", s, 0)
	}
	if title != "" {
		g.Put(x+2, y, " "+Truncate(title, w-6)+" ", Style{FG: tfg, Bold: true}, 0)
	}
}

// HLine draws ├────┤ (or plain ─ when l/r are empty).
func (g *Grid) HLine(x, y, w int, fg, l, r string) {
	if w < 2 {
		return
	}
	if l == "" {
		g.Put(x, y, strings.Repeat("─", w), Style{FG: fg}, 0)
		return
	}
	g.Put(x, y, l+strings.Repeat("─", w-2)+r, Style{FG: fg}, 0)
}

// Hover paints a clickable region's background, keeping the text.
func (g *Grid) Hover(x, y, w, h int, bg string) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			if c := g.At(xx, yy); c != nil {
				c.BG = bg
				c.Bold = true
			}
		}
	}
}

// Dim recolors everything (used behind dialogs).
func (g *Grid) Dim(fg string, keepBG map[string]bool, bg string) {
	for y := range g.c {
		for x := range g.c[y] {
			c := &g.c[y][x]
			c.FG, c.Bold = fg, false
			if !keepBG[c.BG] {
				c.BG = bg
			}
		}
	}
}

// PutANSI draws a string that contains SGR escape codes (output of bubbles
// components) at x,y, clipped to maxw cells. Only colors/bold/reverse are
// interpreted; defBG is used where the string resets the background.
func (g *Grid) PutANSI(x, y int, s string, maxw int, defFG, defBG string) {
	fg, bg, bold, rev := defFG, defBG, false, false
	end := x + maxw
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j >= len(s) {
				return
			}
			if s[j] == 'm' {
				fg, bg, bold, rev = applySGR(s[i+2:j], fg, bg, bold, rev, defFG, defBG)
			}
			i = j + 1
			continue
		}
		if s[i] == 0x1b { // other escape: skip it
			i += 2
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		ch := Clean(string(r))
		if ch == "" {
			continue
		}
		w := runewidth.StringWidth(ch)
		if x+w > end {
			return
		}
		f, b := fg, bg
		if rev {
			f, b = b, f
		}
		g.Put(x, y, ch, Style{FG: f, BG: b, Bold: bold}, 0)
		x += w
	}
}

func applySGR(params, fg, bg string, bold, rev bool, defFG, defBG string) (string, string, bool, bool) {
	if params == "" {
		return defFG, defBG, false, false
	}
	p := strings.Split(params, ";")
	for k := 0; k < len(p); k++ {
		n, _ := strconv.Atoi(p[k])
		switch {
		case n == 0:
			fg, bg, bold, rev = defFG, defBG, false, false
		case n == 1:
			bold = true
		case n == 22:
			bold = false
		case n == 7:
			rev = true
		case n == 27:
			rev = false
		case n == 39:
			fg = defFG
		case n == 49:
			bg = defBG
		case (n == 38 || n == 48) && k+4 < len(p) && p[k+1] == "2":
			r, _ := strconv.Atoi(p[k+2])
			gg, _ := strconv.Atoi(p[k+3])
			b, _ := strconv.Atoi(p[k+4])
			hex := "#" + hex2(r) + hex2(gg) + hex2(b)
			if n == 38 {
				fg = hex
			} else {
				bg = hex
			}
			k += 4
		case (n == 38 || n == 48) && k+2 < len(p) && p[k+1] == "5":
			k += 2 // 256-colour: ignore (components are forced to true colour)
		}
	}
	return fg, bg, bold, rev
}

func hex2(v int) string {
	const d = "0123456789abcdef"
	return string([]byte{d[(v>>4)&15], d[v&15]})
}

// Render produces the final string using the terminal's colour profile.
// Each run of same-styled cells becomes prefix + text + reset; the colour
// escape for a style is computed once and cached (converting colours for a
// 256/16-colour console is expensive).
func (g *Grid) Render(p termenv.Profile) string {
	var sb strings.Builder
	sb.Grow(g.W * g.H * 3)
	for y, row := range g.c {
		x := 0
		for x < len(row) {
			c := row[x]
			pre := stylePrefix(p, c.FG, c.BG, c.Bold)
			sb.WriteString(pre)
			j := x
			for j < len(row) && row[j].FG == c.FG && row[j].BG == c.BG && row[j].Bold == c.Bold {
				sb.WriteString(cellText(row, j))
				j++
			}
			if pre != "" {
				sb.WriteString(resetSeq)
			}
			x = j
		}
		if y < len(g.c)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

const resetSeq = termenv.CSI + termenv.ResetSeq + "m"

type styleKey struct {
	p      termenv.Profile
	fg, bg string
	bold   bool
}

var (
	styleMu    sync.RWMutex
	styleCache = map[styleKey]string{}
)

// stylePrefix is the escape sequence that starts a run in this style
// (identical to what termenv.Style.Styled would emit).
func stylePrefix(p termenv.Profile, fg, bg string, bold bool) string {
	k := styleKey{p, fg, bg, bold}
	styleMu.RLock()
	s, ok := styleCache[k]
	styleMu.RUnlock()
	if ok {
		return s
	}
	st := termenv.Style{}.Foreground(p.Color(fg)).Background(p.Color(bg))
	if bold {
		st = st.Bold()
	}
	out := st.Styled("\x00")
	if i := strings.IndexByte(out, 0); i >= 0 {
		s = out[:i]
	}
	styleMu.Lock()
	styleCache[k] = s
	styleMu.Unlock()
	return s
}

// runeStr avoids an allocation for ASCII runes.
func runeStr(r rune) string {
	if r < 128 {
		return asciiStr[r]
	}
	return string(r)
}

var asciiStr = func() (a [128]string) {
	for i := range a {
		a[i] = string(rune(i))
	}
	return
}()

// cellText is what a cell contributes to the output. It repairs broken wide
// characters (one half overwritten by later drawing) so every row is always
// exactly W cells wide.
func cellText(row []Cell, x int) string {
	c := &row[x]
	if c.cont {
		if x > 0 && !row[x-1].cont && row[x-1].wide {
			return ""
		}
		return " "
	}
	if c.wide {
		if x+1 < len(row) && row[x+1].cont {
			return c.Ch
		}
		return " "
	}
	if c.Ch == "" {
		return " "
	}
	return c.Ch
}
