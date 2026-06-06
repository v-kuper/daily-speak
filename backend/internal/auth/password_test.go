package auth

import "testing"

func TestVerifyPasswordAcceptsNodeScryptHash(t *testing.T) {
	hash := "scrypt$16384$8$1$00112233445566778899aabbccddeeff$711eee554934accfe70e833979342354314b54280d44bc4366156ef5efd9ac81bd285060f7e5328175a6110b9160ee050c6254b9b4d744aa04e0fde3024d1a5f"

	if !VerifyPassword("CorrectHorse123!", hash) {
		t.Fatal("expected password to verify against Node.js scrypt hash format")
	}

	if VerifyPassword("wrong-password", hash) {
		t.Fatal("expected wrong password to be rejected")
	}
}

func TestValidateCredentialsMatchesCurrentAPI(t *testing.T) {
	creds, err := ValidateCredentials("  USER@Example.COM  ", "  SmokeTest123!  ")
	if err != nil {
		t.Fatalf("expected valid credentials: %v", err)
	}
	if creds.Email != "user@example.com" {
		t.Fatalf("expected normalized email, got %q", creds.Email)
	}
	if creds.Password != "SmokeTest123!" {
		t.Fatalf("expected trimmed password, got %q", creds.Password)
	}

	if _, err := ValidateCredentials("bad-email", "123"); err == nil {
		t.Fatal("expected invalid credentials to fail")
	}
}
