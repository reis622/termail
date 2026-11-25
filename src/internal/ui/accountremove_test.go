package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"termail/internal/account"
	"termail/internal/i18n"
)

// selectAcctRow puts the folder cursor on account ai's row.
func selectAcctRow(t *testing.T, m *Model, ai int) int {
	t.Helper()
	m.focus = focusFolders
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowAcct && r.acct == ai {
			m.fcursor = i
			return i
		}
	}
	t.Fatal("no account row")
	return -1
}

func seedSecrets(t *testing.T, email string) {
	t.Helper()
	account.UseMemoryKeyring()
	if err := account.SetSecret(email, account.SecretRefresh, "refresh-x"); err != nil {
		t.Fatal(err)
	}
	if err := account.SetSecret(email, account.SecretPassword, "pass-x"); err != nil {
		t.Fatal(err)
	}
}

func secretsGone(email string) bool {
	_, e1 := account.GetSecret(email, account.SecretRefresh)
	_, e2 := account.GetSecret(email, account.SecretPassword)
	return e1 != nil && e2 != nil
}

func TestAccountRowDelHintClickRemovesAndDeletesSecrets(t *testing.T) {
	m, _ := twoAccounts(t)
	seedSecrets(t, "ben@firma.com")
	seedSecrets(t, "ben@kisisel.com")
	selectAcctRow(t, m, 1)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Del") || !strings.Contains(v, "hesabı kaldır") {
		t.Fatal("Del hint missing on an account row")
	}
	clickKeybar(t, m, "delete")
	if m.confirm == nil || !strings.Contains(m.confirm.text, "ben@firma.com") {
		t.Fatal("clicking the Del hint did not ask to remove the account")
	}
	m.confirm.key(m, "enter")
	if len(m.accts) != 1 || m.accts[0].Cfg.Email != "ben@kisisel.com" {
		t.Fatalf("account not removed: %d", len(m.accts))
	}
	if !secretsGone("ben@firma.com") {
		t.Fatal("token / password left in the credential store")
	}
	if secretsGone("ben@kisisel.com") {
		t.Fatal("the other account's sign-in was deleted")
	}
}

func TestAccountRowDelKeyWithConfig(t *testing.T) {
	t.Setenv("TERMAIL_CONFIG_DIR", t.TempDir())
	m, _ := twoAccounts(t)
	cfg := &account.Config{Accounts: []account.Account{m.accts[0].Cfg, m.accts[1].Cfg}}
	m.opt.Config = cfg
	seedSecrets(t, "ben@kisisel.com")
	selectAcctRow(t, m, 0)
	m.onKey(tea.KeyMsg{Type: tea.KeyDelete})
	if m.confirm == nil {
		t.Fatal("Del on an account row must ask")
	}
	m.confirm.key(m, "esc")
	if len(m.accts) != 2 || secretsGone("ben@kisisel.com") {
		t.Fatal("cancel must keep everything")
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyDelete})
	m.confirm.key(m, "enter")
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].Email != "ben@firma.com" || !secretsGone("ben@kisisel.com") {
		t.Fatal("config / secrets not removed")
	}
}

func TestAccountRowRightClickSignOut(t *testing.T) {
	m, _ := twoAccounts(t)
	seedSecrets(t, "ben@firma.com")
	idx := selectAcctRow(t, m, 1)
	m.focus = focusList
	_ = m.View()
	var hx, hy int
	found := false
	for _, h := range m.hits.list {
		if h.kind == hitFolderRow && h.idx == idx {
			hx, hy, found = h.x, h.y, true
		}
	}
	if !found {
		t.Fatal("account row not on screen")
	}
	m.Update(tea.MouseMsg{X: hx, Y: hy, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	if m.ctxMenu == nil {
		t.Fatal("no context menu on the account row")
	}
	pick := -1
	for i, it := range m.ctxMenu.items {
		if it.label == "Çıkış yap" {
			pick = i
		}
	}
	if pick < 0 {
		t.Fatal("no Çıkış yap item")
	}
	if m.focus != focusFolders || !strings.Contains(ansi.Strip(m.View()), "hesabı kaldır") {
		t.Fatal("right-clicked account row not selected / hint missing")
	}
	// click the menu item
	_ = m.View()
	for _, h := range m.hits.list {
		if h.kind == hitCtxItem && h.idx == pick {
			m.Update(tea.MouseMsg{X: h.x, Y: h.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		}
	}
	if m.confirm == nil || !strings.Contains(m.confirm.text, "ben@firma.com") {
		t.Fatal("Çıkış yap must open the remove confirmation")
	}
	m.confirm.key(m, "enter")
	if len(m.accts) != 1 || !secretsGone("ben@firma.com") {
		t.Fatal("sign out did not remove the account and its sign-in")
	}
}

func TestAccountRemoveEnglishAndOtherRows(t *testing.T) {
	i18n.Set(i18n.EN)
	t.Cleanup(func() { i18n.Set(i18n.TR) })
	m, _ := twoAccounts(t)
	selectAcctRow(t, m, 0)
	if !strings.Contains(ansi.Strip(m.View()), "remove account") {
		t.Fatal("English hint missing")
	}
	m.showFolderMenu(5, 5, m.folderRowsSelectable()[m.fcursor])
	if m.ctxMenu == nil || m.ctxMenu.items[len(m.ctxMenu.items)-1].label != "Sign out" {
		t.Fatal("English menu item missing")
	}
	m.ctxMenu = nil
	// a folder row has no Del hint
	for i, r := range m.folderRowsSelectable() {
		if r.kind == rowFolder {
			m.fcursor = i
			break
		}
	}
	if strings.Contains(ansi.Strip(m.View()), "remove account") {
		t.Fatal("Del hint shown on a folder row")
	}
}
