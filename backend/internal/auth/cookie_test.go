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
	cookie := NewSessionCookieWithConfig(config, "unchanged-session-token", expires)
	if cookie.Name != "daily_speaking_session" || cookie.Value != "unchanged-session-token" || !cookie.Expires.Equal(expires) {
		t.Fatalf("session identity or expiry changed: %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteNoneMode || cookie.Domain != ".example.com" || cookie.Path != "/" {
		t.Fatalf("unexpected cookie: %#v", cookie)
	}
	cleared := ClearSessionCookieWithConfig(config)
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
			cookie := NewSessionCookieWithConfig(config, "token", time.Unix(2_000_000_000, 0))
			if cookie.Secure != tc.wantSecure || cookie.SameSite != tc.wantSameSite || cookie.Domain != tc.wantDomain {
				t.Fatalf("unexpected cookie: %#v", cookie)
			}
		})
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

func TestCookieCompatibilityHelpersUseExplicitLaxDefaults(t *testing.T) {
	for _, env := range []string{"", "development", "production"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("NODE_ENV", env)
			for _, cookie := range []*http.Cookie{NewSessionCookie("token", time.Unix(2_000_000_000, 0)), ClearSessionCookie()} {
				if cookie.Name != "daily_speaking_session" || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" || cookie.Path != "/" || !cookie.HttpOnly {
					t.Fatalf("compatibility behavior changed: %#v", cookie)
				}
			}
		})
	}
}
