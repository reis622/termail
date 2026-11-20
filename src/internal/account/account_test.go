package account

import (
	"strings"
	"testing"

	"termail/internal/discover"
)

func TestSecretsChunked(t *testing.T) {
	UseMemoryKeyring()
	big := strings.Repeat("abcdefghij", 500)
	if err := SetSecret("A@x.com", SecretRefresh, big); err != nil {
		t.Fatal(err)
	}
	got, err := GetSecret("a@x.com", SecretRefresh)
	if err != nil || got != big {
		t.Fatalf("roundtrip failed: %v len=%d", err, len(got))
	}
	SetSecret("a@x.com", SecretRefresh, "short")
	if got, _ := GetSecret("a@x.com", SecretRefresh); got != "short" {
		t.Fatal(got)
	}
	if err := DeleteSecret("a@x.com", SecretRefresh); err != nil {
		t.Fatal(err)
	}
	if _, err := GetSecret("a@x.com", SecretRefresh); err == nil {
		t.Fatal("still there")
	}
}

func TestConfigNoSecrets(t *testing.T) {
	t.Setenv("TERMAIL_CONFIG_DIR", t.TempDir())
	c, _ := Load()
	c.Accounts = append(c.Accounts, Account{Name: c.DefaultName("Gmail"), Email: "a@gmail.com", Settings: discover.Settings{Provider: "Gmail"}})
	if c.DefaultName("Gmail") != "Gmail 2" {
		t.Fatal(c.DefaultName("Gmail"))
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := Load()
	if err != nil || len(c2.Accounts) != 1 || c2.Accounts[0].Email != "a@gmail.com" {
		t.Fatalf("%+v %v", c2, err)
	}
}
