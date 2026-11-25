package ui

import (
	"strings"

	"termail/internal/grid"
	"termail/internal/i18n"
)

// Anthracite palette from the approved previews (no pure black).
const (
	cBG       = "#1f2227"
	cPanel    = "#23272d"
	cRaised   = "#2a2f36"
	cSel      = "#323a45"
	cSelFocus = "#34465a"
	cBorder   = "#353b43" // inactive panel border (quiet)
	cBorderF  = "#5b7896"
	cActive   = cAccent // active panel border: unmistakable
	cFG       = "#d5d8dd"
	cDim      = "#8b929c"
	cFaint    = "#5f6670"
	cAccent   = "#7fa7cc"
	cGreen    = "#93c08f"
	cYellow   = "#d9bb72"
	cRed      = "#d48a86"
	cPurple   = "#b49ad6"
	cBar      = "#191c20"
	cKey      = "#2e353e"
	cToastBG  = "#26302c"
	cToastBd  = "#5d7f6a"
	cDimText  = "#555c66"
	cWaitBG   = "#263241"
	cWaitBd   = "#4f6a86"
	cErrBG    = "#30262a"
	cErrBd    = "#7d5a5c"
	cLinkMark = "#5b7896" // inline link numbers: a dim accent
	cTextSel  = "#41607f" // selected text (fields and the reading pane)
	cHover    = "#3a4e62" // mouse is over a clickable target
	cChip     = "#3d6f9c" // solid fill of the active account in the top bar
	cChipFG   = "#f4f7fb" // contrasting text on that fill
)

func st(fg string) grid.Style                { return grid.Style{FG: fg} }
func stb(fg string) grid.Style               { return grid.Style{FG: fg, Bold: true} }
func stbg(fg, bg string) grid.Style          { return grid.Style{FG: fg, BG: bg} }
func stbgb(fg, bg string, b bool) grid.Style { return grid.Style{FG: fg, BG: bg, Bold: b} }

type keyHint struct{ key, label string }

// keybar draws the bottom shortcut bar. When hits is set, each hint
// (and "? Yardım") is clickable.
func keybar(g *grid.Grid, y int, items []keyHint, right string, hits *hitMap) {
	g.Fill(0, y, g.W, 1, cBar)
	x := 1
	for _, it := range items {
		need := grid.Width(it.key) + grid.Width(it.label) + 5
		if x+need > g.W-grid.Width(right)-2 {
			break
		}
		k0 := x
		x = g.Put(x, y, " "+it.key+" ", stbgb(cFG, cKey, true), 0)
		x = g.Put(x, y, " "+it.label, stbg(cDim, cBar), 0)
		if hits != nil {
			if ks := hintToKey(it.key); ks != "" {
				hits.add(hit{x: k0, y: y, w: x - k0, h: 1, kind: hitKeybar, s: ks})
			}
		}
		x += 2
	}
	if right != "" {
		rw := grid.Width(right)
		g.RPut(g.W-1, y, right, stbg(cFaint, cBar))
		if hits != nil && strings.HasPrefix(right, "?") {
			hits.add(hit{x: g.W - 1 - rw, y: y, w: rw, h: 1, kind: hitKeybar, s: "?"})
		}
	}
}

// hintToKey maps a keybar label ("Enter", "Shift+G", "→") to the key
// string onKey understands.
func hintToKey(k string) string {
	switch k {
	case "Enter", "→/Enter":
		return "enter"
	case "→":
		return "right"
	case "←":
		return "left"
	case "Esc":
		return "esc"
	case "Tab":
		return "tab"
	case "Shift+G":
		return "G"
	case "Shift+D":
		return "D"
	case "Alt+N":
		return "alt+n"
	case "Ctrl+S":
		return "ctrl+s"
	case "Ctrl+E":
		return "ctrl+e"
	case "Ctrl+B":
		return "ctrl+b"
	case "Ctrl+C":
		return "ctrl+c"
	case "Ctrl+F":
		return "ctrl+f"
	case "Ctrl+K":
		return "ctrl+k"
	case "Ctrl+L":
		return "ctrl+l"
	case "Ctrl+P":
		return "ctrl+p"
	case "Ctrl + O harfi":
		return "ctrl+o"
	case "F2":
		return "f2"
	case "Del":
		return "delete"
	case "↑↓", "↑", "↓":
		return ""
	case "h / Esc":
		return "h"
	case "Enter / x":
		return "enter"
	case "?":
		return "?"
	default:
		return k
	}
}

// panel draws a panel frame. The active panel gets a strong accent border and
// a bold accent title; inactive panels get a quiet grey border and title.
func panel(g *grid.Grid, x, y, w, h int, active bool, bg, title string) {
	bd, tf := cBorder, cFaint
	if active {
		bd, tf = cActive, cAccent
	}
	g.Box(x, y, w, h, bd, bg, "", "")
	if title != "" && w > 6 {
		g.Put(x+2, y, " "+grid.Truncate(title, w-6)+" ", grid.Style{FG: tf, Bold: active}, 0)
	}
}

// withComposeHint puts the new-mail shortcut on a keybar unless it is
// already there, so every pane can show a clickable "n Yeni".
func withComposeHint(items []keyHint) []keyHint {
	for _, it := range items {
		if it.key == "n" {
			return items
		}
	}
	out := make([]keyHint, 0, len(items)+1)
	out = append(out, keyHint{"n", i18n.T("Yeni")})
	return append(out, items...)
}
