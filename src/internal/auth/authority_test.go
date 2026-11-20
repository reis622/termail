package auth

import (
	"net/url"
	"strings"
	"testing"
)

func TestAuthorityAndLoginHint(t *testing.T) {
	cases := []struct{ email, authority string }{
		{"ahmet@outlook.com", "/consumers/"},
		{"ahmet@hotmail.com", "/consumers/"},
		{"a@hotmail.co.uk", "/consumers/"},
		{"a@outlook.com.tr", "/consumers/"},
		{"a@live.com", "/consumers/"},
		{"a@msn.com", "/consumers/"},
		{"ahmet@firma.com.tr", "/common/"},
		{"a@live.example.com", "/common/"},
	}
	for _, c := range cases {
		cfg, err := ConfigFor(Microsoft, "id", c.email)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(AuthURL(cfg, "http://localhost:1234/", "st", "ch", "  "+strings.ToUpper(c.email)+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(u.Path, c.authority) {
			t.Errorf("%s: %s", c.email, u.Path)
		}
		q := u.Query()
		if q.Get("login_hint") != c.email || q.Get("prompt") != "select_account" || q.Get("domain_hint") != "" {
			t.Errorf("%s: query %v", c.email, q)
		}
		if cfg.Endpoint.TokenURL[:len(cfg.Endpoint.TokenURL)-len("oauth2/v2.0/token")] != cfg.Endpoint.AuthURL[:len(cfg.Endpoint.AuthURL)-len("oauth2/v2.0/authorize")] {
			t.Errorf("%s: token / auth authority differ", c.email)
		}
	}
	// junk never reaches login_hint
	cfg, _ := ConfigFor(Microsoft, "id", "x@outlook.com")
	for _, bad := range []string{"ah\x00met@outlook.com", "ahmet @outlook.com", "ahmet@@outlook.com", "ahmet", "a\u0007@outlook.com"} {
		u, _ := url.Parse(AuthURL(cfg, "http://localhost/", "s", "c", bad))
		if h := u.Query().Get("login_hint"); h != "" {
			t.Errorf("%q gave login_hint %q", bad, h)
		}
	}
}
