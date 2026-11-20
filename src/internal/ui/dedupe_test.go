package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"termail/internal/backend"
)

func rowKeys(m *Model) []string {
	var out []string
	for _, r := range m.folderRows() {
		if r.kind == rowSep || r.kind == rowBlank {
			continue
		}
		out = append(out, fmt.Sprintf("%d|%d|%s|%s", r.kind, r.acct, r.folder, r.label))
	}
	return out
}

func TestFolderRowsStableUnderNavigationAndRefresh(t *testing.T) {
	m := nastyModel(t)
	m.applySize(120, 20)
	press(m, "left")
	before := rowKeys(m)
	seen := map[string]bool{}
	for _, k := range before {
		if seen[k] {
			t.Fatalf("duplicate folder row %q", k)
		}
		seen[k] = true
	}
	folders := append([]backend.Folder(nil), m.accts[0].Folders...)
	for i := 0; i < 40; i++ {
		press(m, []string{"down", "up", "down"}[i%3])
		// refreshes and status updates arriving all the time, some with the
		// same mailbox listed twice by the server
		dup := append(append([]backend.Folder(nil), folders...), folders[0], folders[2])
		m.onFolders(foldersMsg{acct: 0, folders: dup})
		m.onFolders(foldersMsg{acct: 1, folders: folders})
		after := rowKeys(m)
		if len(after) != len(before) {
			t.Fatalf("step %d: %d rows, want %d\n%v", i, len(after), len(before), after)
		}
	}
	unified := 0
	for _, r := range m.folderRows() {
		if r.kind == rowUnified && !r.read {
			unified++
		}
	}
	if unified != 1 {
		t.Fatalf("Tüm Gelen Kutuları shown %d times", unified)
	}
	// the screen shows each folder once too
	v := ansi.Strip(m.View())
	if n := strings.Count(v, unifiedTitle); n < 1 || n > 2 {
		// left panel, and the F1 chip in the account bar when several accounts exist
		t.Fatalf("%q appears %d times on screen", unifiedTitle, n)
	}
}

func TestListRefreshReplacesNotAppends(t *testing.T) {
	m := nastyModel(t)
	m.applySize(120, 20)
	envs := append([]backend.Envelope(nil), m.accts[0].Lists["INBOX"]...)
	n := len(envs)
	for i := 0; i < 10; i++ {
		press(m, "down")
		m.onList(listMsg{acct: 0, folder: "INBOX", envs: append(append([]backend.Envelope(nil), envs...), envs[0], envs[1])})
		if got := len(m.accts[0].Lists["INBOX"]); got != n {
			t.Fatalf("refresh %d: %d mails, want %d", i, got, n)
		}
	}
	if m.cursor != 10 {
		t.Fatalf("cursor moved to %d after refreshes", m.cursor)
	}
	v := ansi.Strip(m.View())
	if c := strings.Count(v, "okunmamış"); c != 1 {
		t.Fatalf("count line shown %d times", c)
	}
}
