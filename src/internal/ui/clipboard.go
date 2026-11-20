package ui

import (
	"runtime"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"

	"termail/internal/edit"
	"termail/internal/toast"
)

// Clipboard. Copy writes the Windows clipboard directly (CGO-free, proper
// Unicode, no "clip" program) and also sends the terminal's own copy code
// (OSC 52), which covers SSH sessions and other systems. Paste reads the
// Windows clipboard; terminals that paste themselves (Windows Terminal
// catches Ctrl+V, right-click in many terminals) send the text as a
// bracketed paste or as typed keys, which the fields take as well.

var readClipboardFn = readClipboard

func copyText(s string) {
	if runtime.GOOS == "windows" {
		if clipboard.WriteAll(s) == nil {
			return
		}
	}
	termenv.Copy(s)
	if runtime.GOOS != "windows" {
		_ = clipboard.WriteAll(s) // when xclip / wl-copy is there
	}
}

func readClipboard() (string, error) { return clipboard.ReadAll() }

// pasteDedupe: Windows Terminal turns Ctrl+V into a paste of its own; when
// a terminal both pastes and passes Ctrl+V on, the same text would arrive
// twice. Whichever comes second within this window is dropped.
const pasteDedupe = 700 * time.Millisecond

type pasteSrc int

const (
	pasteKey      pasteSrc = iota + 1 // Ctrl+V / right-click: read the clipboard ourselves
	pasteTerminal                     // the terminal sent the text (bracketed paste)
)

type lastPaste struct {
	text string
	src  pasteSrc
	at   time.Time
}

func pasteKeyOf(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
}

// dupPaste reports whether this paste repeats one that just came the other way.
func (m *Model) dupPaste(text string, src pasteSrc) bool {
	now := m.opt.Now()
	k := pasteKeyOf(text)
	lp := m.lastPaste
	m.lastPaste = lastPaste{text: k, src: src, at: now}
	return lp.src != 0 && lp.src != src && lp.text == k && now.Sub(lp.at) < pasteDedupe
}

// pasteInto reads the clipboard into a field (Ctrl+V, right-click).
func (m *Model) pasteInto(e *edit.Editor) tea.Cmd {
	s, err := readClipboardFn()
	if err != nil {
		m.note(toast.Error, "Pano okunamadı.")
		return nil
	}
	if s == "" {
		m.note(toast.Info, "Pano boş.")
		return nil
	}
	if m.dupPaste(s, pasteKey) || m.justTyped(s) {
		return nil
	}
	m.pasteEcho = []rune(edit.Normalize(s, e.Kind))
	m.echoUntil = m.opt.Now().Add(pasteDedupe)
	if e.Insert(s) {
		m.note(toast.Info, "Yapıştırılan metin çok uzundu; sığan kadarı alındı.")
	}
	return nil
}

// note shows a short message: a toast, and on a compose tab (where toasts
// would cover the text) the status line under the text instead.
func (m *Model) note(kind toast.Kind, text string) {
	if c := m.tabs[m.active].compose; c != nil {
		fg := cGreen
		switch kind {
		case toast.Error:
			fg = cRed
		case toast.Info:
			fg = cYellow
		}
		c.status, c.statusFG, c.noteSet = text, fg, true
	}
	m.toastInfo(kind, "", text)
}

// copied puts text on the clipboard with a short note.
func (m *Model) copied(text, note string) {
	copyTextFn(text)
	if note == "" {
		note = "Kopyalandı"
	}
	m.note(toast.Success, note)
}

// editKey sends a key to a text field and carries out what it asks for
// (copy, paste, a shortened paste). handled: the field used the key.
func (m *Model) editKey(e *edit.Editor, k tea.KeyMsg) (bool, tea.Cmd) {
	if k.Paste && m.dupPaste(string(k.Runes), pasteTerminal) {
		return true, nil
	}
	if k.Type == tea.KeyRunes && !k.Paste && m.eatEcho(k.Runes) {
		return true, nil
	}
	r := e.Key(k)
	switch {
	case r.Copy != "":
		m.copied(r.Copy, "")
	case r.Paste:
		return true, m.pasteInto(e)
	case r.Cut:
		m.note(toast.Info, "Yapıştırılan metin çok uzundu; sığan kadarı alındı.")
	}
	return r.Handled, nil
}

// Key-by-key pastes. Windows Terminal and conhost replay pasted text as key
// presses (no bracketed paste on the Windows console). If a terminal both
// passes Ctrl+V on (we read the clipboard) and replays the text, the second
// copy is dropped, in either order.

// eatEcho drops replayed keys that repeat the paste we just did.
func (m *Model) eatEcho(rs []rune) bool {
	now := m.opt.Now()
	m.noteTyped(rs, now)
	if len(m.pasteEcho) == 0 || now.After(m.echoUntil) {
		m.pasteEcho = nil
		return false
	}
	for _, r := range rs {
		if len(m.pasteEcho) == 0 || m.pasteEcho[0] != r {
			m.pasteEcho = nil
			return false
		}
		m.pasteEcho = m.pasteEcho[1:]
	}
	m.echoUntil = now.Add(pasteDedupe)
	return true
}

// noteTyped keeps the current fast burst of typed keys (gaps under 100 ms).
func (m *Model) noteTyped(rs []rune, now time.Time) {
	if now.Sub(m.typedAt) > 100*time.Millisecond {
		m.typedRecent = m.typedRecent[:0]
	}
	m.typedAt = now
	if len(m.typedRecent) < 4096 {
		m.typedRecent = append(m.typedRecent, rs...)
	}
}

// justTyped: the clipboard text just arrived as replayed keys.
func (m *Model) justTyped(s string) bool {
	k := []rune(strings.Join(strings.Fields(s), ""))
	if len(k) < 2 || m.opt.Now().Sub(m.typedAt) > pasteDedupe {
		return false
	}
	var t []rune
	for _, r := range m.typedRecent {
		if r != ' ' && r != '\t' {
			t = append(t, r)
		}
	}
	return len(t) >= len(k) && string(t[len(t)-len(k):]) == string(k)
}
