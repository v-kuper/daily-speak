package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/workqueue"
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
	guestAssetID := uuid.NewString()
	if _, err := database.Exec(ctx, `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, object_key, content_type,
		   expected_size_bytes, expected_checksum_sha256, retention_until)
		VALUES ($1, $2, 'guest_preview_audio', 'ready', 'local', $3, 'audio/webm', 4, $4, $5)`,
		guestAssetID, guest.Identity.PrincipalID, "v1/guest/recording/"+guestAssetID+".webm",
		strings.Repeat("a", 64), config.withDefaults().now().Add(config.withDefaults().GuestTTL)); err != nil {
		t.Fatalf("insert guest media asset: %v", err)
	}
	readyPreviewID := uuid.NewString()
	readyPreviewJobID := uuid.NewString()
	now := config.withDefaults().now()
	if err := workqueue.Enqueue(ctx, database, workqueue.NewJob{
		ID: readyPreviewJobID, Kind: workqueue.KindGuestPreview, ResourceID: readyPreviewID,
		IdempotencyKey: "guest.preview:" + readyPreviewID, MaxAttempts: 2, AvailableAt: now,
	}); err != nil {
		t.Fatalf("enqueue ready guest preview: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO guest_previews
		  (id, guest_principal_id, audio_asset_id, topic, duration, recording_timestamp,
		   practice_type, state, transcript, preview_corrections, preview_job_id,
		   idempotency_key, request_digest, expires_at)
		VALUES
		  ($1, $2, $3, 'First impression', 12, $4, 'free_talk', 'ready',
		   'A reusable guest transcript.', '[{"ruleId":"preview-1"}]'::jsonb,
		   $5, 'ready-preview', $6, $7)`,
		readyPreviewID, guest.Identity.PrincipalID, guestAssetID, now,
		readyPreviewJobID, strings.Repeat("b", 64), now.Add(config.withDefaults().GuestTTL)); err != nil {
		t.Fatalf("insert ready guest preview: %v", err)
	}
	interviewSessionID := uuid.NewString()
	if _, err := database.Exec(ctx, `
		INSERT INTO interview_sessions
		  (id, owner_principal_id, create_key, request_digest, topic, opening_question, status, max_duration_seconds,
		   guest_preview_id)
		VALUES ($1, $2, 'integration-interview', $3, 'First impression', 'How did it go?', 'finalized', 60, $4)`,
		interviewSessionID, guest.Identity.PrincipalID, strings.Repeat("c", 64), readyPreviewID); err != nil {
		t.Fatalf("insert guest interview timeline: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO interview_turns
		  (id, session_id, seq, question, asked_at_ms, ended_at_ms, final_transcript)
		VALUES ($1, $2, 1, 'How did it go?', 0, 12000, 'A reusable guest transcript.')`,
		uuid.NewString(), interviewSessionID); err != nil {
		t.Fatalf("insert guest interview turn: %v", err)
	}

	email := fmt.Sprintf("mobile-%s@example.com", uuid.NewString())
	credentials, err := ValidateCredentials(email, "password123")
	if err != nil {
		t.Fatal(err)
	}
	registered, err := RegisterIdentityUser(ctx, database, config, credentials, &guest.Identity, DeviceInfo{Name: "Personal iPhone", Platform: "IOS"})
	if err != nil {
		t.Fatalf("register mobile user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, registered.Identity.PrincipalID)
	})
	if registered.Identity.Kind != "user" || registered.Identity.User == nil || registered.Identity.User.Email != email || registered.Session.Platform != "ios" {
		t.Fatalf("unexpected registered grant: %+v", registered)
	}
	if registered.GuestPreviewPromotion == nil || registered.GuestPreviewPromotion.Status != "promoted" || registered.GuestPreviewPromotion.RecordingID != readyPreviewID {
		t.Fatalf("unexpected guest promotion result: %+v", registered.GuestPreviewPromotion)
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
	var assetOwner, assetPurpose string
	var assetAttached bool
	var retentionCleared bool
	if err := database.QueryRow(ctx, `
		SELECT owner_principal_id, purpose, attached_at IS NOT NULL, retention_until IS NULL
		FROM media_assets WHERE id = $1`, guestAssetID).Scan(
		&assetOwner, &assetPurpose, &assetAttached, &retentionCleared,
	); err != nil || assetOwner != registered.Identity.PrincipalID || assetPurpose != "recording_audio" || !assetAttached || !retentionCleared {
		t.Fatalf("guest media promotion owner=%q purpose=%q attached=%t retentionCleared=%t err=%v", assetOwner, assetPurpose, assetAttached, retentionCleared, err)
	}
	var previewState, promotedRecordingID, promotedJobID string
	if err := database.QueryRow(ctx, `
		SELECT state, promoted_recording_id, promoted_job_id
		FROM guest_previews WHERE id = $1`, readyPreviewID).Scan(
		&previewState, &promotedRecordingID, &promotedJobID,
	); err != nil || previewState != "promoted" || promotedRecordingID != readyPreviewID || promotedJobID == "" {
		t.Fatalf("ready preview promotion state=%q recording=%q job=%q err=%v", previewState, promotedRecordingID, promotedJobID, err)
	}
	var recordingOwner, recordingTranscript, recordingStage, recordingAudioAssetID, recordingJobID string
	var recordingSuggestions string
	if err := database.QueryRow(ctx, `
		SELECT user_id, transcript, processing_stage, suggestions::text, audio_asset_id, processing_job_id
		FROM recordings WHERE id = $1`, readyPreviewID).Scan(
		&recordingOwner, &recordingTranscript, &recordingStage, &recordingSuggestions,
		&recordingAudioAssetID, &recordingJobID,
	); err != nil || recordingOwner != registered.Identity.PrincipalID || recordingTranscript != "A reusable guest transcript." || recordingStage != "suggestions" || recordingSuggestions != "[]" || recordingAudioAssetID != guestAssetID || recordingJobID != promotedJobID {
		t.Fatalf("promoted ready recording owner=%q transcript=%q stage=%q suggestions=%q audio=%q job=%q err=%v", recordingOwner, recordingTranscript, recordingStage, recordingSuggestions, recordingAudioAssetID, recordingJobID, err)
	}
	var timelineRecordingID, timelineUserID string
	if err := database.QueryRow(ctx, `
		SELECT recording_id, user_id FROM interview_sessions WHERE id = $1`,
		interviewSessionID).Scan(&timelineRecordingID, &timelineUserID); err != nil ||
		timelineRecordingID != readyPreviewID || timelineUserID != registered.Identity.PrincipalID {
		t.Fatalf("promoted interview timeline recording=%q user=%q err=%v", timelineRecordingID, timelineUserID, err)
	}
	var guestJobState string
	if err := database.QueryRow(ctx, `SELECT state FROM processing_jobs WHERE id = $1`, readyPreviewJobID).Scan(&guestJobState); err != nil || guestJobState != "cancelled" {
		t.Fatalf("ready preview job state=%q err=%v", guestJobState, err)
	}
	retryTx, err := database.Begin(ctx)
	if err != nil {
		t.Fatalf("begin promotion retry: %v", err)
	}
	if _, err := promoteGuestPreview(ctx, retryTx, guest.Identity.PrincipalID, registered.Identity.PrincipalID, now); err != nil {
		_ = retryTx.Rollback(ctx)
		t.Fatalf("retry promoted preview: %v", err)
	}
	if err := retryTx.Commit(ctx); err != nil {
		t.Fatalf("commit promotion retry: %v", err)
	}
	var fullJobCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM processing_jobs
		WHERE kind = 'recording.process' AND resource_id = $1
		  AND idempotency_key = $2`, readyPreviewID, "guest.preview.promote:"+readyPreviewID).Scan(&fullJobCount); err != nil || fullJobCount != 1 {
		t.Fatalf("ready preview full jobs=%d err=%v", fullJobCount, err)
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

	// A completed guest upload that was never attached to a preview is not
	// duration-verified. Merging the identity must leave that object under guest
	// ownership so it cannot be submitted as an ordinary account recording.
	strayGuest, err := CreateAnonymousIdentity(ctx, database, config, DeviceInfo{Name: "Unverified upload", Platform: "android"})
	if err != nil {
		t.Fatal(err)
	}
	strayAssetID := uuid.NewString()
	if _, err := database.Exec(ctx, `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, object_key, content_type,
		   expected_size_bytes, expected_checksum_sha256, retention_until)
		VALUES ($1, $2, 'guest_preview_audio', 'ready', 'local', $3, 'audio/webm', 4, $4, $5)`,
		strayAssetID, strayGuest.Identity.PrincipalID,
		"v1/guest/recording/"+strayAssetID+".webm", strings.Repeat("e", 64), now.Add(config.withDefaults().GuestTTL)); err != nil {
		t.Fatalf("insert unverified guest media: %v", err)
	}
	strayLogin, err := LoginIdentityUser(ctx, database, config, credentials, &strayGuest.Identity, DeviceInfo{Name: "Pixel stray", Platform: "android"})
	if err != nil {
		t.Fatalf("login with unverified guest upload: %v", err)
	}
	if strayLogin.GuestPreviewPromotion == nil || strayLogin.GuestPreviewPromotion.Status != "no_preview" {
		t.Fatalf("unexpected unverified upload result: %+v", strayLogin.GuestPreviewPromotion)
	}
	if err := database.QueryRow(ctx, `
		SELECT owner_principal_id, purpose FROM media_assets WHERE id = $1`, strayAssetID).Scan(&assetOwner, &assetPurpose); err != nil || assetOwner != strayGuest.Identity.PrincipalID || assetPurpose != "guest_preview_audio" {
		t.Fatalf("unverified guest media crossed ownership boundary owner=%q purpose=%q err=%v", assetOwner, assetPurpose, err)
	}
	if revoked, err := RevokeDeviceSession(ctx, database, registered.Identity.PrincipalID, strayLogin.Session.ID, "test_cleanup"); err != nil || !revoked {
		t.Fatalf("revoke stray login: revoked=%t err=%v", revoked, err)
	}

	secondGuest, err := CreateAnonymousIdentity(ctx, database, config, DeviceInfo{Name: "Android preview", Platform: "android"})
	if err != nil {
		t.Fatal(err)
	}
	queuedAssetID := uuid.NewString()
	if _, err := database.Exec(ctx, `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, object_key, content_type,
		   expected_size_bytes, expected_checksum_sha256, retention_until)
		VALUES ($1, $2, 'guest_preview_audio', 'ready', 'local', $3, 'audio/webm', 4, $4, $5)`,
		queuedAssetID, secondGuest.Identity.PrincipalID,
		"v1/guest/recording/"+queuedAssetID+".webm", strings.Repeat("c", 64), now.Add(config.withDefaults().GuestTTL)); err != nil {
		t.Fatalf("insert queued guest media: %v", err)
	}
	queuedPreviewID := uuid.NewString()
	queuedPreviewJobID := uuid.NewString()
	if err := workqueue.Enqueue(ctx, database, workqueue.NewJob{
		ID: queuedPreviewJobID, Kind: workqueue.KindGuestPreview, ResourceID: queuedPreviewID,
		IdempotencyKey: "guest.preview:" + queuedPreviewID, MaxAttempts: 2, AvailableAt: now,
	}); err != nil {
		t.Fatalf("enqueue queued guest preview: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO guest_previews
		  (id, guest_principal_id, audio_asset_id, topic, duration, recording_timestamp,
		   practice_type, state, preview_job_id, idempotency_key, request_digest, expires_at)
		VALUES ($1, $2, $3, 'Queued preview', 9, $4, 'topic', 'queued',
		        $5, 'queued-preview', $6, $7)`,
		queuedPreviewID, secondGuest.Identity.PrincipalID, queuedAssetID, now,
		queuedPreviewJobID, strings.Repeat("d", 64), now.Add(config.withDefaults().GuestTTL)); err != nil {
		t.Fatalf("insert queued guest preview: %v", err)
	}
	loggedIn, err := LoginIdentityUser(ctx, database, config, credentials, &secondGuest.Identity, DeviceInfo{Name: "Pixel", Platform: "Android"})
	if err != nil {
		t.Fatalf("login and merge: %v", err)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, secondGuest.AccessToken); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("login merge left guest active: %v", err)
	}
	if _, err := AuthenticateAccessToken(ctx, database, config, loggedIn.AccessToken); err != nil {
		t.Fatalf("login access failed: %v", err)
	}
	if loggedIn.GuestPreviewPromotion == nil || loggedIn.GuestPreviewPromotion.Status != "promoted" || loggedIn.GuestPreviewPromotion.RecordingID != queuedPreviewID {
		t.Fatalf("existing-account guest promotion failed: %+v", loggedIn.GuestPreviewPromotion)
	}
	var queuedRecordingCount int
	if err := database.QueryRow(ctx, `
		SELECT COUNT(*) FROM recordings WHERE id = $1 AND user_id = $2`,
		queuedPreviewID, registered.Identity.PrincipalID,
	).Scan(&queuedRecordingCount); err != nil || queuedRecordingCount != 1 {
		t.Fatalf("existing-account guest promotion created recordings=%d err=%v", queuedRecordingCount, err)
	}
	if err := database.QueryRow(ctx, `SELECT state FROM processing_jobs WHERE id = $1`, queuedPreviewJobID).Scan(&guestJobState); err != nil || guestJobState != "cancelled" {
		t.Fatalf("queued preview job state=%q err=%v", guestJobState, err)
	}
	if err := database.QueryRow(ctx, `
		SELECT owner_principal_id, purpose FROM media_assets WHERE id = $1`, queuedAssetID).Scan(&assetOwner, &assetPurpose); err != nil || assetOwner != registered.Identity.PrincipalID || assetPurpose != "recording_audio" {
		t.Fatalf("promoted preview media owner=%q purpose=%q err=%v", assetOwner, assetPurpose, err)
	}

	otherDevice, err := LoginIdentityUser(ctx, database, config, credentials, nil, DeviceInfo{Name: "iPad", Platform: "iPadOS"})
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

func TestGuestPreviewPromotionReservesAccountQuota(t *testing.T) {
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
	config := TokenConfig{SigningKey: []byte(strings.Repeat("guest-quota-secret-", 3))}
	credentials, err := ValidateCredentials(fmt.Sprintf("quota-%s@example.com", uuid.NewString()), "password123")
	if err != nil {
		t.Fatal(err)
	}
	account, err := RegisterIdentityUser(ctx, database, config, credentials, nil, DeviceInfo{Name: "Account", Platform: "ios"})
	if err != nil {
		t.Fatalf("register account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, account.Identity.PrincipalID)
	})
	if _, err := database.Exec(ctx, `
		INSERT INTO recordings (id, user_id, topic, duration, timestamp, transcript)
		VALUES ($1, $2, 'Existing usage', $3, NOW(), '')`,
		uuid.NewString(), account.Identity.PrincipalID, quota.FreeWeeklyLimitSeconds-5); err != nil {
		t.Fatalf("insert existing quota usage: %v", err)
	}

	guest, err := CreateAnonymousIdentity(ctx, database, config, DeviceInfo{Name: "Quota preview", Platform: "android"})
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	assetID := uuid.NewString()
	now := config.withDefaults().now()
	if _, err := database.Exec(ctx, `
		INSERT INTO media_assets
		  (id, owner_principal_id, purpose, state, storage_driver, object_key, content_type,
		   expected_size_bytes, expected_checksum_sha256, retention_until)
		VALUES ($1, $2, 'guest_preview_audio', 'ready', 'local', $3, 'audio/webm', 4, $4, $5)`,
		assetID, guest.Identity.PrincipalID, "v1/guest/recording/"+assetID+".webm",
		strings.Repeat("f", 64), now.Add(config.withDefaults().GuestTTL)); err != nil {
		t.Fatalf("insert guest media: %v", err)
	}
	previewID := uuid.NewString()
	previewJobID := uuid.NewString()
	if err := workqueue.Enqueue(ctx, database, workqueue.NewJob{
		ID: previewJobID, Kind: workqueue.KindGuestPreview, ResourceID: previewID,
		IdempotencyKey: "guest.preview:" + previewID, MaxAttempts: 2, AvailableAt: now,
	}); err != nil {
		t.Fatalf("enqueue preview: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO guest_previews
		  (id, guest_principal_id, audio_asset_id, topic, duration, recording_timestamp,
		   practice_type, state, transcript, preview_job_id, idempotency_key, request_digest, expires_at)
		VALUES ($1, $2, $3, 'Quota preview', 12, $4, 'free_talk', 'ready',
		        'Quota transcript.', $5, 'quota-preview', $6, $7)`,
		previewID, guest.Identity.PrincipalID, assetID, now, previewJobID,
		strings.Repeat("1", 64), now.Add(config.withDefaults().GuestTTL)); err != nil {
		t.Fatalf("insert preview: %v", err)
	}

	loggedIn, err := LoginIdentityUser(ctx, database, config, credentials, &guest.Identity, DeviceInfo{Name: "Quota login", Platform: "android"})
	if err != nil {
		t.Fatalf("login with quota-bound preview: %v", err)
	}
	if loggedIn.GuestPreviewPromotion == nil || loggedIn.GuestPreviewPromotion.Status != "not_promoted" || loggedIn.GuestPreviewPromotion.Reason != "quota_exceeded" {
		t.Fatalf("unexpected quota promotion result: %+v", loggedIn.GuestPreviewPromotion)
	}
	var promotedRecordings int
	if err := database.QueryRow(ctx, `SELECT COUNT(*) FROM recordings WHERE id = $1`, previewID).Scan(&promotedRecordings); err != nil || promotedRecordings != 0 {
		t.Fatalf("quota rejection created recordings=%d err=%v", promotedRecordings, err)
	}
}
