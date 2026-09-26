package media

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestURLSignerAcceptsOnlyUntamperedUnexpiredRequest(t *testing.T) {
	now := time.Date(2026, 9, 26, 20, 0, 0, 0, time.UTC)
	signer, err := NewURLSigner([]byte(strings.Repeat("media-signing-secret-", 2)))
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	signer.now = func() time.Time { return now }
	path := "/api/v1/media/uploads/upload-1/parts/1"
	signed := signer.Sign(http.MethodPut, path, url.Values{
		"sizeBytes":      {"4"},
		"checksumSha256": {strings.Repeat("a", 64)},
	}, now.Add(5*time.Minute))
	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if !signer.Verify(http.MethodPut, parsed.Path, parsed.Query()) {
		t.Fatal("fresh signed request was rejected")
	}
	tampered := parsed.Query()
	tampered.Set("sizeBytes", "5")
	if signer.Verify(http.MethodPut, parsed.Path, tampered) {
		t.Fatal("tampered signed request was accepted")
	}
	if signer.Verify(http.MethodGet, parsed.Path, parsed.Query()) {
		t.Fatal("signature was accepted for a different method")
	}
	signer.now = func() time.Time { return now.Add(6 * time.Minute) }
	if signer.Verify(http.MethodPut, parsed.Path, parsed.Query()) {
		t.Fatal("expired signed request was accepted")
	}
}

func TestURLSignerRequiresStrongSecret(t *testing.T) {
	if _, err := NewURLSigner([]byte("short")); err == nil {
		t.Fatal("short signing secret was accepted")
	}
}
