package ui

import (
	"net/mail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termail/internal/mailmsg"
)

func TestOpenInBrowserWritesHTMLAndUsesSelectedMail(t *testing.T) {
	var opened []string
	old := openFileFn
	openFileFn = func(p string) error { opened = append(opened, p); return nil }
	defer func() { openFileFn = old }()

	m, a := testModel(t)
	p := &mailmsg.Parsed{
		From:    []*mail.Address{{Name: "Ali", Address: "ali@x.com"}},
		Subject: "Üç",
		Text:    "gövde <&>",
	}
	m.bodies.Put(bodyKey{0, "INBOX", 3}, p)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if seen(a, 3) {
		t.Fatal("o must not mark read until the browser actually opens")
	}
	if len(m.tabs) != 1 {
		t.Fatal("o must not open a Termail tab")
	}
	path := filepath.Join(os.TempDir(), "Termail", "3.html")
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("cached body must write HTML immediately, before the open command: %v", err)
	}
	completeOpen(t, m, cmd)
	if !seen(a, 3) {
		t.Fatal("o should mark the selected mail read once the browser opens")
	}
	if !sameUIDs(visUIDs(m), 3, 2, 1) {
		t.Fatalf("Gelen Kutusu after o must keep the mail: %v", visUIDs(m))
	}
	if e, _ := m.selected(); e.UID != 3 {
		t.Fatalf("cursor should stay on the opened mail, got %d", e.UID)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("HTML file: %v", err)
	}
	out := string(b)
	for _, want := range []string{"Kimden", "Ali", "Konu", "Üç", "<pre", "gövde &lt;&amp;&gt;"} {
		if !strings.Contains(out, want) {
			t.Fatalf("HTML missing %q\n%s", want, out)
		}
	}
	if len(opened) == 0 {
		t.Fatal("the file was not handed to the default program")
	}
	if !strings.HasSuffix(opened[0], "3.html") {
		t.Fatalf("opened the wrong file: %v", opened)
	}
}

func runUICmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; i < 8 && cmd != nil; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		// tea.Batch produces a slice of Cmd values (unexported batchMsg).
		if cmds := cmdsOf(msg); cmds != nil {
			for _, c := range cmds {
				runUICmd(t, m, c)
			}
			return
		}
		_, cmd = m.Update(msg)
	}
}

// completeOpen runs commands until the browser-open result is applied,
// without draining the flag-op timer that follows a successful mark-read.
func completeOpen(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; i < 8 && cmd != nil; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if cmds := cmdsOf(msg); cmds != nil {
			for _, c := range cmds {
				completeOpen(t, m, c)
			}
			return
		}
		_, cmd = m.Update(msg)
		if _, ok := msg.(openBrowserDone); ok {
			return
		}
	}
}

func cmdsOf(msg tea.Msg) []tea.Cmd {
	if msg == nil {
		return nil
	}
	rv := reflect.ValueOf(msg)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	var out []tea.Cmd
	for i := 0; i < rv.Len(); i++ {
		if c, ok := rv.Index(i).Interface().(tea.Cmd); ok {
			out = append(out, c)
		}
	}
	return out
}
