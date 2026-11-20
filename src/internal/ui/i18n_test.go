package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"termail/internal/account"
	"termail/internal/i18n"
)

func TestHelpLanguageToggle(t *testing.T) {
	i18n.Set(i18n.TR)
	t.Cleanup(func() { i18n.Set(i18n.TR) })
	dir := t.TempDir()
	t.Setenv("TERMAIL_CONFIG_DIR", dir)
	m, _ := testModel(t)
	m.w, m.h, m.realW, m.realH = 140, 40, 140, 40
	m.help = true
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Kısayollar") {
		t.Fatalf("turkish help missing title, got %q", trim(v))
	}
	m.onKey(asKey("l"))
	if i18n.Get() != i18n.EN {
		t.Fatalf("L should switch to English, got %q", i18n.Get())
	}
	v = ansi.Strip(m.View())
	if !strings.Contains(v, "Shortcuts") {
		t.Fatalf("english help missing title: %q", trim(v))
	}
	if !strings.Contains(v, "Language") {
		t.Fatalf("english help missing language control: %q", trim(v))
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"language"`) || !strings.Contains(string(b), "en") {
		t.Fatalf("language not persisted: %s", b)
	}
	cfg, err := account.Load()
	if err != nil || cfg.Language != "en" {
		t.Fatalf("load language: %+v %v", cfg, err)
	}
	// keybar L is mouse-clickable
	m.help = true
	_ = m.View()
	var hx, hy int
	found := false
	for _, h := range m.hits.list {
		if h.kind == hitKeybar && (h.s == "L" || h.s == "l") {
			hx, hy = h.x, h.y
			found = true
			break
		}
	}
	if !found {
		t.Fatal("help keybar has no L hit")
	}
	m.Update(tea.MouseMsg{X: hx, Y: hy, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if i18n.Get() != i18n.TR {
		t.Fatalf("clicking L should switch back to Turkish, got %q", i18n.Get())
	}
}

func trim(s string) string {
	if len(s) > 400 {
		return s[:400]
	}
	return s
}
