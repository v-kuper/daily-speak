package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAccessTokenRoundTripAndTamperProtection(t *testing.T) {
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	config := TokenConfig{SigningKey: []byte(strings.Repeat("a", 48)), Now: func() time.Time { return now }}
	token, expiresAt, err := IssueAccessToken(config, "principal-1", "session-1", "user")
	if err != nil {
		t.Fatal(err)
	}
	if expiresAt.Sub(now) != defaultAccessTTL {
		t.Fatalf("unexpected access TTL: %s", expiresAt.Sub(now))
	}
	claims, err := ParseAccessToken(config, token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.PrincipalID != "principal-1" || claims.SessionID != "session-1" || claims.Kind != "user" || claims.TokenID == "" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	parts := strings.Split(token, ".")
	parts[1] = strings.Repeat("A", len(parts[1]))
	if _, err := ParseAccessToken(config, strings.Join(parts, ".")); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("tampered token error = %v", err)
	}
	expiredConfig := config
	expiredConfig.Now = func() time.Time { return expiresAt }
	if _, err := ParseAccessToken(expiredConfig, token); !errors.Is(err, ErrAccessTokenExpired) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestTokenConfigFromEnv(t *testing.T) {
	t.Setenv("AUTH_ACCESS_TOKEN_SECRET", "")
	config, err := TokenConfigFromEnv()
	if err != nil || config.Enabled() {
		t.Fatalf("empty secret should disable mobile identity without failing startup: enabled=%v err=%v", config.Enabled(), err)
	}

	t.Setenv("AUTH_ACCESS_TOKEN_SECRET", "too-short")
	if _, err := TokenConfigFromEnv(); err == nil {
		t.Fatal("weak signing secret was accepted")
	}

	t.Setenv("AUTH_ACCESS_TOKEN_SECRET", strings.Repeat("s", 48))
	t.Setenv("AUTH_ACCESS_TOKEN_TTL", "10m")
	t.Setenv("AUTH_REFRESH_TOKEN_TTL", "240h")
	t.Setenv("AUTH_GUEST_TOKEN_TTL", "2h")
	config, err = TokenConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled() || config.AccessTTL != 10*time.Minute || config.RefreshTTL != 240*time.Hour || config.GuestTTL != 2*time.Hour {
		t.Fatalf("unexpected config: %+v", config)
	}

	t.Setenv("AUTH_ACCESS_TOKEN_TTL", "never")
	if _, err := TokenConfigFromEnv(); err == nil {
		t.Fatal("invalid duration was accepted")
	}
}

func TestOpaqueRefreshTokensAreRandomAndHashable(t *testing.T) {
	first, firstHash, err := newRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := newRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "dsr_v1_") || first == second || firstHash == secondHash {
		t.Fatalf("unexpected refresh tokens: first=%q second=%q", first, second)
	}
	if firstHash == first || firstHash != hashRefreshToken(first) || len(firstHash) != 64 {
		t.Fatal("refresh token hash is not deterministic SHA-256")
	}
}
