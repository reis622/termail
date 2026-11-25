// Package account stores account settings (no secrets) in config.json and
// secrets (passwords, refresh tokens) in the OS credential store
// (Windows Credential Manager via go-keyring).
package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zalando/go-keyring"

	"termail/internal/discover"
	"termail/internal/i18n"
)

// Account is one mailbox. It never contains a password or token.
type Account struct {
	Name     string            `json:"name"` // shown in the folder panel, e.g. "Kişisel"
	Email    string            `json:"email"`
	Username string            `json:"username,omitempty"` // login name if different from Email
	FullName string            `json:"full_name,omitempty"`
	Settings discover.Settings `json:"settings"`
}

// Login returns the user name used to sign in.
func (a Account) Login() string {
	if a.Username != "" {
		return a.Username
	}
	return a.Email
}

// Config is the whole config.json.
type Config struct {
	Accounts   []Account `json:"accounts"`
	MSClientID string    `json:"microsoft_client_id,omitempty"`
	// Google OAuth client (optional; release builds carry their own).
	GoogleClientID     string `json:"google_client_id,omitempty"`
	GoogleClientSecret string `json:"google_client_secret,omitempty"`
	// Mouse: "kapalı" turns the mouse off (default: on).
	Mouse string `json:"fare,omitempty"`
	// Language is "tr" or "en". Empty means auto-detect on start.
	Language string `json:"language,omitempty"`
}

// MouseOn reports whether mouse support is wanted ("fare": "kapalı" turns
// it off; "kapali", "off", "hayır", "false", "0" work too).
func (c *Config) MouseOn() bool {
	if c == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(c.Mouse)) {
	case "kapalı", "kapali", "kapat", "off", "hayır", "hayir", "false", "0", "no":
		return false
	}
	return true
}

// Dir returns %APPDATA%\Termail (or ~/.config/termail).
func Dir() (string, error) {
	if d := os.Getenv("TERMAIL_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Termail"), nil
}

// Load reads config.json (an empty config if it does not exist).
func Load() (*Config, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(d, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes config.json atomically.
func (c *Config) Save() error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(d, "config.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(d, "config.json"))
}

// DefaultName picks a friendly unique account name.
// SaveLanguage records the UI language in config.json without dropping
// other settings (used when the language is switched from the help screen,
// including demo mode).
func SaveLanguage(lang string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	c.Language = lang
	return c.Save()
}

func (c *Config) DefaultName(provider string) string {
	if provider == "" {
		provider = i18n.T("Hesap")
	}
	taken := map[string]bool{}
	for _, a := range c.Accounts {
		taken[a.Name] = true
	}
	if !taken[provider] {
		return provider
	}
	for i := 2; ; i++ {
		n := provider + " " + strconv.Itoa(i)
		if !taken[n] {
			return n
		}
	}
}

// Secret kinds.
const (
	SecretPassword = "password"
	SecretRefresh  = "refresh_token"
)

const chunk = 1200 // Windows Credential Manager limits a blob to ~2.5 KB (UTF-16)

func service(email string) string { return "Termail:" + strings.ToLower(email) }

// SetSecret stores a secret, splitting it into chunks when large.
func SetSecret(email, kind, value string) error {
	svc := service(email)
	_ = DeleteSecret(email, kind)
	if len(value) <= chunk {
		return keyring.Set(svc, kind, value)
	}
	n := 0
	for i := 0; i < len(value); i += chunk {
		end := min(i+chunk, len(value))
		if err := keyring.Set(svc, fmt.Sprintf("%s#%d", kind, n), value[i:end]); err != nil {
			return err
		}
		n++
	}
	return keyring.Set(svc, kind, fmt.Sprintf("\x00chunks:%d", n))
}

// GetSecret reads a secret stored with SetSecret.
func GetSecret(email, kind string) (string, error) {
	svc := service(email)
	v, err := keyring.Get(svc, kind)
	if err != nil {
		return "", err
	}
	if rest, ok := strings.CutPrefix(v, "\x00chunks:"); ok {
		n, err := strconv.Atoi(rest)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for i := 0; i < n; i++ {
			p, err := keyring.Get(svc, fmt.Sprintf("%s#%d", kind, i))
			if err != nil {
				return "", err
			}
			sb.WriteString(p)
		}
		return sb.String(), nil
	}
	return v, nil
}

// DeleteSecret removes a secret and its chunks.
func DeleteSecret(email, kind string) error {
	svc := service(email)
	v, err := keyring.Get(svc, kind)
	if err != nil {
		return err
	}
	if rest, ok := strings.CutPrefix(v, "\x00chunks:"); ok {
		n, _ := strconv.Atoi(rest)
		for i := 0; i < n; i++ {
			_ = keyring.Delete(svc, fmt.Sprintf("%s#%d", kind, i))
		}
	}
	return keyring.Delete(svc, kind)
}

// UseMemoryKeyring switches to an in-memory store (demo / tests only).
func UseMemoryKeyring() { keyring.MockInit() }
