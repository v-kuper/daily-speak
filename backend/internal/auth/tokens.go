package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultTokenIssuer   = "daily-speaking-api"
	defaultTokenAudience = "daily-speaking-mobile"
	defaultAccessTTL     = 15 * time.Minute
	defaultRefreshTTL    = 30 * 24 * time.Hour
	defaultGuestTTL      = 24 * time.Hour
	minimumSigningKeyLen = 32
)

var (
	ErrIdentityUnavailable = errors.New("mobile identity is not configured")
	ErrInvalidAccessToken  = errors.New("access token is invalid")
	ErrAccessTokenExpired  = errors.New("access token has expired")
	ErrInvalidRefreshToken = errors.New("refresh token is invalid")
	ErrRefreshTokenExpired = errors.New("refresh token has expired")
	ErrRefreshTokenReused  = errors.New("refresh token reuse detected")
	ErrInvalidGuest        = errors.New("guest identity is invalid")
)

type TokenConfig struct {
	SigningKey []byte
	Issuer     string
	Audience   string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	GuestTTL   time.Duration
	Now        func() time.Time
}

func TokenConfigFromEnv() (TokenConfig, error) {
	config := TokenConfig{
		SigningKey: []byte(strings.TrimSpace(os.Getenv("AUTH_ACCESS_TOKEN_SECRET"))),
		Issuer:     strings.TrimSpace(os.Getenv("AUTH_ACCESS_TOKEN_ISSUER")),
		Audience:   strings.TrimSpace(os.Getenv("AUTH_ACCESS_TOKEN_AUDIENCE")),
	}
	var err error
	if config.AccessTTL, err = durationFromEnv("AUTH_ACCESS_TOKEN_TTL", defaultAccessTTL); err != nil {
		return TokenConfig{}, err
	}
	if config.RefreshTTL, err = durationFromEnv("AUTH_REFRESH_TOKEN_TTL", defaultRefreshTTL); err != nil {
		return TokenConfig{}, err
	}
	if config.GuestTTL, err = durationFromEnv("AUTH_GUEST_TOKEN_TTL", defaultGuestTTL); err != nil {
		return TokenConfig{}, err
	}
	config = config.withDefaults()
	if len(config.SigningKey) > 0 && len(config.SigningKey) < minimumSigningKeyLen {
		return TokenConfig{}, fmt.Errorf("AUTH_ACCESS_TOKEN_SECRET must contain at least %d characters", minimumSigningKeyLen)
	}
	return config, nil
}

func durationFromEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", name)
	}
	return value, nil
}

func (c TokenConfig) Enabled() bool {
	return len(c.SigningKey) >= minimumSigningKeyLen
}

func (c TokenConfig) withDefaults() TokenConfig {
	if c.Issuer == "" {
		c.Issuer = defaultTokenIssuer
	}
	if c.Audience == "" {
		c.Audience = defaultTokenAudience
	}
	if c.AccessTTL <= 0 {
		c.AccessTTL = defaultAccessTTL
	}
	if c.RefreshTTL <= 0 {
		c.RefreshTTL = defaultRefreshTTL
	}
	if c.GuestTTL <= 0 {
		c.GuestTTL = defaultGuestTTL
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

func (c TokenConfig) now() time.Time {
	return c.withDefaults().Now().UTC()
}

type AccessClaims struct {
	PrincipalID string `json:"sub"`
	SessionID   string `json:"sid"`
	Kind        string `json:"kind"`
	Issuer      string `json:"iss"`
	Audience    string `json:"aud"`
	ExpiresAt   int64  `json:"exp"`
	IssuedAt    int64  `json:"iat"`
	TokenID     string `json:"jti"`
}

func IssueAccessToken(config TokenConfig, principalID string, sessionID string, kind string) (string, time.Time, error) {
	config = config.withDefaults()
	if !config.Enabled() {
		return "", time.Time{}, ErrIdentityUnavailable
	}
	if kind != "guest" && kind != "user" {
		return "", time.Time{}, ErrInvalidAccessToken
	}
	issuedAt := config.now()
	expiresAt := issuedAt.Add(config.AccessTTL)
	claims := AccessClaims{
		PrincipalID: principalID,
		SessionID:   sessionID,
		Kind:        kind,
		Issuer:      config.Issuer,
		Audience:    config.Audience,
		ExpiresAt:   expiresAt.Unix(),
		IssuedAt:    issuedAt.Unix(),
		TokenID:     uuid.NewString(),
	}
	headerJSON := []byte(`{"alg":"HS256","typ":"JWT"}`)
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	unsigned := header + "." + payload
	signature := signToken(config.SigningKey, unsigned)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), expiresAt, nil
}

func ParseAccessToken(config TokenConfig, token string) (AccessClaims, error) {
	config = config.withDefaults()
	if !config.Enabled() {
		return AccessClaims{}, ErrIdentityUnavailable
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(providedSignature, signToken(config.SigningKey, parts[0]+"."+parts[1])) {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if json.Unmarshal(headerBytes, &header) != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	var claims AccessClaims
	if json.Unmarshal(payloadBytes, &claims) != nil || claims.Issuer != config.Issuer || claims.Audience != config.Audience || claims.PrincipalID == "" || claims.SessionID == "" || claims.TokenID == "" || (claims.Kind != "guest" && claims.Kind != "user") {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	now := config.now()
	if claims.ExpiresAt <= now.Unix() {
		return AccessClaims{}, ErrAccessTokenExpired
	}
	if claims.IssuedAt > now.Add(time.Minute).Unix() || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt {
		return AccessClaims{}, ErrInvalidAccessToken
	}
	return claims, nil
}

func signToken(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func newRefreshToken() (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	token := "dsr_v1_" + base64.RawURLEncoding.EncodeToString(random)
	return token, hashRefreshToken(token), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
