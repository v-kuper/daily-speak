package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestCookieConfigFromEnv(t *testing.T) {
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	t.Setenv("SESSION_COOKIE_SAME_SITE", "none")
	t.Setenv("SESSION_COOKIE_DOMAIN", ".example.com")
	config, err := CookieConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Unix(2_000_000_000, 0)
	cookie := NewRefreshCookieWithConfig(config, "unchanged-refresh-token", expires)
	if cookie.Name != RefreshCookieName || cookie.Value != "unchanged-refresh-token" || !cookie.Expires.Equal(expires) {
		t.Fatalf("refresh identity or expiry changed: %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteNoneMode || cookie.Domain != ".example.com" || cookie.Path != "/api/v1/auth" {
		t.Fatalf("unexpected cookie: %#v", cookie)
	}
	cleared := ClearRefreshCookieWithConfig(config)
	if cleared.Name != cookie.Name || cleared.Value != "" || cleared.MaxAge != -1 || cleared.Path != cookie.Path || cleared.Domain != cookie.Domain || cleared.Secure != cookie.Secure || cleared.SameSite != cookie.SameSite || !cleared.HttpOnly {
		t.Fatalf("clearing cookie must expire the same scope: %#v", cleared)
	}
}

func TestCookieConfigDefaultsAndExplicitSettings(t *testing.T) {
	for _, tc := range []struct {
		name, secure, sameSite, domain string
		wantSecure                     bool
		wantSameSite                   http.SameSite
		wantDomain                     string
	}{
		{name: "defaults ignore NODE_ENV", wantSameSite: http.SameSiteLaxMode},
		{name: "explicit insecure lax", secure: "false", sameSite: "lax", wantSameSite: http.SameSiteLaxMode},
		{name: "strict", secure: "true", sameSite: "strict", wantSecure: true, wantSameSite: http.SameSiteStrictMode},
		{name: "trim and case", secure: " TRUE ", sameSite: " None ", domain: " .example.com ", wantSecure: true, wantSameSite: http.SameSiteNoneMode, wantDomain: ".example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NODE_ENV", "production")
			t.Setenv("SESSION_COOKIE_SECURE", tc.secure)
			t.Setenv("SESSION_COOKIE_SAME_SITE", tc.sameSite)
			t.Setenv("SESSION_COOKIE_DOMAIN", tc.domain)
			config, err := CookieConfigFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			cookie := NewRefreshCookieWithConfig(config, "token", time.Unix(2_000_000_000, 0))
			if cookie.Secure != tc.wantSecure || cookie.SameSite != tc.wantSameSite || cookie.Domain != tc.wantDomain {
				t.Fatalf("unexpected cookie: %#v", cookie)
			}
		})
	}
}

func TestBrowserRefreshCookieIsHttpOnlyAndNarrowlyScoped(t *testing.T) {
	config := CookieConfig{Secure: true, SameSite: http.SameSiteLaxMode, Domain: ".example.com"}
	expires := time.Now().UTC().Add(time.Hour)
	cookie := NewRefreshCookieWithConfig(config, "rotating-refresh-token", expires)
	if cookie.Name != RefreshCookieName || cookie.Value != "rotating-refresh-token" {
		t.Fatalf("unexpected refresh identity: %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/api/v1/auth" || cookie.Domain != config.Domain || cookie.SameSite != config.SameSite {
		t.Fatalf("refresh cookie is not safely scoped: %#v", cookie)
	}
	cleared := ClearRefreshCookieWithConfig(config)
	if cleared.Name != cookie.Name || cleared.Path != cookie.Path || cleared.Domain != cookie.Domain || cleared.MaxAge != -1 || !cleared.HttpOnly {
		t.Fatalf("refresh cookie cleanup changed scope: %#v", cleared)
	}
}

func TestSameSiteNoneRequiresSecureCookie(t *testing.T) {
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	t.Setenv("SESSION_COOKIE_SAME_SITE", "none")
	t.Setenv("SESSION_COOKIE_DOMAIN", "")
	if _, err := CookieConfigFromEnv(); err == nil {
		t.Fatal("expected SameSite=None without Secure to fail")
	}
}

func TestCookieConfigRejectsInvalidSettings(t *testing.T) {
	for _, tc := range []struct{ name, secure, sameSite, domain string }{
		{name: "invalid boolean", secure: "maybe", sameSite: "lax"},
		{name: "invalid same site", secure: "true", sameSite: "sometimes"},
		{name: "domain URL", secure: "true", sameSite: "lax", domain: "https://example.com"},
		{name: "domain path", secure: "true", sameSite: "lax", domain: "example.com/path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SESSION_COOKIE_SECURE", tc.secure)
			t.Setenv("SESSION_COOKIE_SAME_SITE", tc.sameSite)
			t.Setenv("SESSION_COOKIE_DOMAIN", tc.domain)
			if _, err := CookieConfigFromEnv(); err == nil {
				t.Fatal("expected invalid setting to fail")
			}
		})
	}
}
