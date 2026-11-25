package auth

import (
	"context"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"termail/internal/i18n"
)

// Google OAuth client of this build. Both are empty in the source code; a
// release build sets them with
//
//	go build -ldflags "-X termail/internal/auth.googleClientID=<id> -X termail/internal/auth.googleClientSecret=<secret>"
//
// For a desktop app Google calls the secret "not confidential", but it is
// still kept out of the repository. TERMAIL_GOOGLE_CLIENT_ID /
// TERMAIL_GOOGLE_CLIENT_SECRET or "google_client_id" / "google_client_secret"
// in config.json override it.
var (
	googleClientID     string
	googleClientSecret string
)

// Google is the Gmail / Google account sign-in.
const Google Provider = "google"

// GoogleEndpoint is Google's OAuth 2.0 endpoint for installed apps.
var GoogleEndpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// GoogleScopes: full IMAP / SMTP access (the only Gmail scope that allows
// XOAUTH2) plus the signed-in address.
var GoogleScopes = []string{"https://mail.google.com/", "openid", "email"}

// Environment variables that override the built-in Google client.
const (
	EnvGoogleClientID     = "TERMAIL_GOOGLE_CLIENT_ID"
	EnvGoogleClientSecret = "TERMAIL_GOOGLE_CLIENT_SECRET"
)

// GoogleCredentials picks the Google client: config.json first, then the
// environment, then the build. The secret always comes from the same place
// as the ID so a pair is never mixed.
func GoogleCredentials(cfgID, cfgSecret string) (id, secret string) {
	for _, c := range [][2]string{
		{cfgID, cfgSecret},
		{os.Getenv(EnvGoogleClientID), os.Getenv(EnvGoogleClientSecret)},
		{googleClientID, googleClientSecret},
	} {
		if id := strings.TrimSpace(c[0]); id != "" {
			return id, strings.TrimSpace(c[1])
		}
	}
	return "", ""
}

// GoogleEnabled reports whether this build can sign in to Google accounts
// (Google desktop clients need both the ID and the secret).
func GoogleEnabled(cfgID, cfgSecret string) bool {
	id, secret := GoogleCredentials(cfgID, cfgSecret)
	return id != "" && secret != ""
}

// GoogleConfig returns the OAuth2 configuration for Google, or
// ErrNoClientID when this build has no Google client.
func GoogleConfig(cfgID, cfgSecret string) (*oauth2.Config, error) {
	id, secret := GoogleCredentials(cfgID, cfgSecret)
	if id == "" || secret == "" {
		return nil, ErrNoClientID
	}
	return &oauth2.Config{ClientID: id, ClientSecret: secret, Endpoint: GoogleEndpoint, Scopes: append([]string(nil), GoogleScopes...)}, nil
}

// GoogleUnavailable is the message for a build without a Google client.
func GoogleUnavailable() string {
	return i18n.T("Google ile giriş bu sürümde yapılandırılmamış. Uygulama şifresiyle girebilirsin.")
}

// DetectProvider tells which browser sign-in an address uses: Google for
// gmail.com / googlemail.com, Microsoft for personal Microsoft addresses
// (outlook, hotmail, live, msn …), "" for everything else.
func DetectProvider(email string) Provider {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return ""
	}
	switch email[at+1:] {
	case "gmail.com", "googlemail.com":
		return Google
	}
	if IsConsumer(email) {
		return Microsoft
	}
	return ""
}

// GoogleAuthURL builds Google's sign-in address: PKCE (S256), offline
// access so a refresh token is issued, and the consent screen every time so
// signing in again always yields a new refresh token.
func GoogleAuthURL(cfg *oauth2.Config, redirect, state, challenge, loginHint string) string {
	c := *cfg
	c.RedirectURL = redirect
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "select_account consent"),
	}
	if h := strings.ToLower(strings.TrimSpace(loginHint)); ValidAddress(h) {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", h))
	}
	return c.AuthCodeURL(state, opts...)
}

// GoogleBrowserLogin runs Google's installed-app flow: authorization code +
// PKCE with a loopback redirect to http://127.0.0.1:<random port>.
func GoogleBrowserLogin(ctx context.Context, cfg *oauth2.Config, loginHint string, open func(string) error) (*oauth2.Token, error) {
	return loopbackLogin(ctx, Google, cfg, "127.0.0.1", func(redirect, state, challenge string) string {
		return GoogleAuthURL(cfg, redirect, state, challenge, loginHint)
	}, open)
}

// GoogleTokenSource renews Google access tokens with the saved refresh
// token (silently, shortly before they expire).
func GoogleTokenSource(cfg *oauth2.Config, refresh string, onRefresh func(string)) oauth2.TokenSource {
	return tokenSource(Google, cfg, refresh, onRefresh)
}

// GoogleTokenSourceFrom is GoogleTokenSource seeded with a fresh token.
func GoogleTokenSourceFrom(cfg *oauth2.Config, t *oauth2.Token, onRefresh func(string)) oauth2.TokenSource {
	return tokenSourceFrom(Google, cfg, t, onRefresh)
}

// googleFriendly turns a Google sign-in error into plain words.
func googleFriendly(e *Error) string {
	d := strings.ToLower(e.Desc)
	switch e.OAuth {
	case "access_denied":
		if strings.Contains(d, "verification") || strings.Contains(d, "test") {
			return i18n.T("Google bu hesabın girişine izin vermedi. Hesabın programın test kullanıcıları arasında olmalı.")
		}
		return i18n.T("Giriş iptal edildi.")
	case "invalid_grant":
		return i18n.T("Google oturumunun süresi doldu; yeniden giriş yap.")
	case "invalid_client", "unauthorized_client", "deleted_client":
		return i18n.T("Google bu programın girişine izin vermiyor; program ayarlarında bir eksik var.")
	case "admin_policy_enforced", "org_internal":
		return i18n.T("Kuruluşun bu programın Google hesabına erişmesine izin vermiyor.")
	}
	return i18n.T("Google ile giriş tamamlanamadı. Tekrar dene.")
}
