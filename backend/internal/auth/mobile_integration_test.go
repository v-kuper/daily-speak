package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

func TestMobileIdentityLifecycle(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	database, err := db.Connect(ctx, databaseURL, false)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	config := TokenConfig{SigningKey: []byte(strings.Repeat("integration-secret-", 3))}

	guest, err := CreateAnonymousIdentity(ctx, database, config, DeviceInfo{Name: "iPhone 18", Platform: "iOS"})
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM principals WHERE id = $1`, guest.Identity.PrincipalID)
	})
	guestIdentity, err := AuthenticateAccessToken(ctx, database, config, guest.AccessToken)
	if err != nil || guestIdentity.Kind != "guest" || guestIdentity.User != nil {
		t.Fatalf("authenticate guest: identity=%+v err=%v", guestIdentity, err)
	}

	email := fmt.Sprintf("mobile-%s@example.com", uuid.NewString())
	credentials, err := ValidateCredentials(email, "password123")
	if err != nil {
		t.Fatal(err)
	}
	registered, err := RegisterMobileUser(ctx, database, config, credentials, &guest.Identity, DeviceInfo{Name: "Personal iPhone", Platform: "IOS"})
	if err != nil {
		t.Fatalf("register mobile user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, registered.Identity.PrincipalID)
	})
	if registered.Identity.Kind != "user" || registered.Identity.User == nil || registered.Identity.User.Email != email || registered.Session.Platform != "ios" {
		t.Fatalf("unexpected registered grant: %+v", registered)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, guest.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("merged guest access remains active: %v", err)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, registered.AccessToken); err != nil {
		t.Fatalf("registered access failed: %v", err)
	}
	var mergeTarget string
	if err := database.QueryRow(ctx, `SELECT user_principal_id FROM principal_merges WHERE guest_principal_id = $1`, guest.Identity.PrincipalID).Scan(&mergeTarget); err != nil || mergeTarget != registered.Identity.PrincipalID {
		t.Fatalf("guest merge was not persisted atomically: target=%q err=%v", mergeTarget, err)
	}
	var storedRefreshHash string
	if err := database.QueryRow(ctx, `SELECT token_hash FROM refresh_tokens WHERE session_id = $1 AND consumed_at IS NULL`, registered.Session.ID).Scan(&storedRefreshHash); err != nil {
		t.Fatalf("load refresh hash: %v", err)
	}
	if storedRefreshHash == registered.RefreshToken || storedRefreshHash != hashRefreshToken(registered.RefreshToken) {
		t.Fatal("database did not retain only the refresh token hash")
	}

	rotated, err := RotateRefreshToken(ctx, database, config, registered.RefreshToken)
	if err != nil {
		t.Fatalf("rotate refresh token: %v", err)
	}
	if rotated.RefreshToken == registered.RefreshToken || rotated.Session.ID != registered.Session.ID {
		t.Fatal("rotation did not replace the token inside the same device family")
	}
	if _, err := RotateRefreshToken(ctx, database, config, registered.RefreshToken); !errors.Is(err, ErrRefreshTokenReused) {
		t.Fatalf("refresh replay error = %v", err)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, rotated.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("refresh replay did not revoke access for the device family: %v", err)
	}

	secondGuest, err := CreateAnonymousIdentity(ctx, database, config, DeviceInfo{Name: "Android preview", Platform: "android"})
	if err != nil {
		t.Fatal(err)
	}
	loggedIn, err := LoginMobileUser(ctx, database, config, credentials, &secondGuest.Identity, DeviceInfo{Name: "Pixel", Platform: "Android"})
	if err != nil {
		t.Fatalf("login and merge: %v", err)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, secondGuest.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("login merge left guest active: %v", err)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, loggedIn.AccessToken); err != nil {
		t.Fatalf("login access failed: %v", err)
	}

	otherDevice, err := LoginMobileUser(ctx, database, config, credentials, nil, DeviceInfo{Name: "iPad", Platform: "iPadOS"})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := ListDeviceSessions(ctx, database, loggedIn.Identity.PrincipalID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("active sessions = %d, err=%v", len(sessions), err)
	}
	if revoked, err := RevokeDeviceSession(ctx, database, loggedIn.Identity.PrincipalID, otherDevice.Session.ID, "test"); err != nil || !revoked {
		t.Fatalf("revoke device: revoked=%v err=%v", revoked, err)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, otherDevice.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("revoked device access remains active: %v", err)
	}
	if err := RevokeAllDeviceSessions(ctx, database, loggedIn.Identity.PrincipalID, "test_logout_all"); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, loggedIn.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("logout-all left access active: %v", err)
	}
}
