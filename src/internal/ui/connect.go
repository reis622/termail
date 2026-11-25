package ui

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/oauth2"

	"termail/internal/account"
	"termail/internal/auth"
	"termail/internal/backend"
	"termail/internal/discover"
)

// BuildBackend creates the backend for a saved account, reading its secret
// from the credential store. stNeedsLogin-style errors are returned when the
// secret is missing.
func BuildBackend(cfg *account.Config, a account.Account) (backend.Backend, error) {
	bc := backend.Config{Incoming: a.Settings.Incoming, Outgoing: a.Settings.Outgoing, Username: a.Login()}
	if a.Settings.Auth == discover.AuthMicrosoft {
		rt, err := account.GetSecret(a.Email, account.SecretRefresh)
		if err != nil {
			return nil, err
		}
		oc, err := oauthConfig(cfg, auth.Microsoft, "") // refresh: /common accepts tokens from either authority
		if err != nil {
			return nil, err
		}
		email := a.Email
		ts := auth.TokenSource(oc, rt, func(n string) { _ = account.SetSecret(email, account.SecretRefresh, n) })
		bc.Token = func(ctx context.Context) (string, error) {
			t, err := ts.Token()
			if err != nil {
				return "", err
			}
			return t.AccessToken, nil
		}
	} else if a.Settings.Auth == discover.AuthGoogle {
		rt, err := account.GetSecret(a.Email, account.SecretRefresh)
		if err != nil {
			return nil, err
		}
		oc, err := googleConfig(cfg)
		if err != nil {
			return nil, err
		}
		email := a.Email
		ts := auth.GoogleTokenSource(oc, rt, func(n string) { _ = account.SetSecret(email, account.SecretRefresh, n) })
		bc.Token = func(ctx context.Context) (string, error) {
			t, err := ts.Token()
			if err != nil {
				return "", err
			}
			return t.AccessToken, nil
		}
	} else {
		pw, err := account.GetSecret(a.Email, account.SecretPassword)
		if err != nil {
			return nil, err
		}
		// a password saved by an older version may carry invisible junk
		// (NUL from Ctrl / AltGr presses): never part of a real password
		bc.Password = CleanSecret(pw, a.Settings.Auth == discover.AuthAppPassword)
	}
	return backend.NewIMAP(bc), nil
}

func oauthConfig(cfg *account.Config, p auth.Provider, email string) (*oauth2.Config, error) {
	id := ""
	if cfg != nil {
		id = cfg.MSClientID
	}
	return auth.ConfigFor(p, id, email)
}

// googleConfig is the Google client of this build (config.json, the
// environment or the build itself), or auth.ErrNoClientID.
func googleConfig(cfg *account.Config) (*oauth2.Config, error) {
	id, secret := "", ""
	if cfg != nil {
		id, secret = cfg.GoogleClientID, cfg.GoogleClientSecret
	}
	return auth.GoogleConfig(id, secret)
}

func removeAccountConfig(cfg *account.Config, email string) {
	for i, a := range cfg.Accounts {
		if strings.EqualFold(a.Email, email) {
			cfg.Accounts = append(cfg.Accounts[:i], cfg.Accounts[i+1:]...)
			break
		}
	}
	_ = cfg.Save()
	_ = account.DeleteSecret(email, account.SecretPassword)
	_ = account.DeleteSecret(email, account.SecretRefresh)
}

// Browser and clipboard hooks (replaced in tests).
var (
	openURLFn  = openURL
	copyTextFn = copyText
)

// openURL opens a link in the default browser (see browserCommands for the
// safety rules; the link is never handed to a shell unescaped).
func openURL(u string) error {
	cmds, err := browserCommands(runtime.GOOS, u)
	if err != nil {
		return err
	}
	for i, c := range cmds {
		if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
			if _, err := exec.LookPath(c[0]); err != nil {
				return errors.New("tarayıcı yok")
			}
		}
		if err = exec.Command(c[0], c[1:]...).Start(); err == nil || i == len(cmds)-1 {
			return err
		}
	}
	return err
}
