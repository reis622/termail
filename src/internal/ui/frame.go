package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// clampFrame is the last safety net before a frame goes to the terminal:
// exactly h lines, none wider than w cells (ANSI-aware). A frame that is
// taller or wider than the window makes the terminal wrap or scroll, after
// which the renderer can no longer erase old rows (rows "cloned" on screen).
func clampFrame(s string, w, h int) string {
	if w < 1 || h < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\r", "")
		if ansi.StringWidth(l) > w {
			l = ansi.Truncate(l, w, "")
		}
		lines[i] = l
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// edgeMargin keeps the last terminal column empty. Some Windows consoles
// wrap immediately when a character lands in the last column (instead of
// waiting for the next character); a full-width row would then push the
// cursor down and, on the bottom row, scroll the whole screen.
const edgeMargin = 1
