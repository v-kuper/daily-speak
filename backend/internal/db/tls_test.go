package db

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestParsePoolConfigRequiresVerifiedTLS(t *testing.T) {
	for _, sslmode := range []string{"disable", "prefer", "require", "verify-ca"} {
		t.Run(sslmode, func(t *testing.T) {
			_, err := parsePoolConfig("postgres://user:pass@db.example.com:5432/app?sslmode="+sslmode, true)
			if err == nil || !strings.Contains(err.Error(), "verify-full") {
				t.Fatalf("sslmode=%s: expected verified TLS error, got %v", sslmode, err)
			}
		})
	}
	config, err := parsePoolConfig("postgres://user:pass@db.example.com:5432/app?sslmode=verify-full", true)
	if err != nil {
		t.Fatalf("verify-full config: %v", err)
	}
	if config.ConnConfig.TLSConfig == nil || config.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Fatalf("verify-full config did not verify peer")
	}
	if config.ConnConfig.TLSConfig.ServerName != "db.example.com" {
		t.Fatalf("unexpected TLS server name %q", config.ConnConfig.TLSConfig.ServerName)
	}
}

func TestRejectsUnencryptedFallback(t *testing.T) {
	config, err := parsePoolConfig("postgres://user:pass@db.example.com:5432/app?sslmode=verify-full", true)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	config.ConnConfig.Fallbacks = append(config.ConnConfig.Fallbacks, &pgconn.FallbackConfig{Host: "fallback.example.com", Port: 5432})
	if err := requireVerifiedTLS(config); err == nil {
		t.Fatal("unencrypted fallback was accepted")
	}
}
