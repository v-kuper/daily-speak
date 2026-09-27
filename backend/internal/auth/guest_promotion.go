package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func mergeGuestPrincipal(ctx context.Context, tx pgx.Tx, guest *Identity, userPrincipalID string, now time.Time) (*GuestPreviewPromotion, error) {
	if guest == nil {
		return nil, nil
	}
	guestPrincipalID := strings.TrimSpace(guest.PrincipalID)
	guestSessionID := strings.TrimSpace(guest.SessionID)
	if guest.Kind != "guest" || guestPrincipalID == "" || guestSessionID == "" {
		return nil, ErrInvalidGuest
	}
	var kind string
	var expiresAt time.Time
	var mergedInto *string
	err := tx.QueryRow(ctx, `
		SELECT p.kind, p.expires_at, p.merged_into_principal_id
		FROM principals p
		JOIN device_sessions s ON s.principal_id = p.id
		WHERE p.id = $1 AND s.id = $2
		  AND s.revoked_at IS NULL AND s.expires_at > $3
		FOR UPDATE OF p, s`, guestPrincipalID, guestSessionID, now).Scan(&kind, &expiresAt, &mergedInto)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidGuest
	}
	if err != nil {
		return nil, err
	}
	if kind != "guest" || !expiresAt.After(now) {
		return nil, ErrInvalidGuest
	}
	if mergedInto != nil {
		if *mergedInto == userPrincipalID {
			return nil, nil
		}
		return nil, ErrInvalidGuest
	}
	if _, err := tx.Exec(ctx, `
		UPDATE principals
		SET merged_into_principal_id = $2, merged_at = $3, updated_at = $3
		WHERE id = $1`, guestPrincipalID, userPrincipalID, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO principal_merges (guest_principal_id, user_principal_id, merged_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (guest_principal_id) DO NOTHING`, guestPrincipalID, userPrincipalID, now); err != nil {
		return nil, err
	}
	// Only the asset attached to an eligible preview may cross this ownership
	// boundary. An upload that was never duration-verified stays guest-owned and
	// expires through the normal retention sweep. Promotion locks the preview
	// before its media asset, matching the expiry-maintenance lock order.
	promotion, err := promoteGuestPreview(ctx, tx, guestPrincipalID, userPrincipalID, now)
	if err != nil {
		return nil, err
	}
	if err := revokePrincipalSessionsTx(ctx, tx, guestPrincipalID, "principal_merged", now); err != nil {
		return nil, err
	}
	return promotion, nil
}

// promoteGuestPreview turns the single bounded guest result into the normal
// account recording in the same transaction as the principal merge. The
// preview ID is retained as the recording ID so clients can keep polling the
// same stable resource after authentication. Preview corrections are not
// copied: the account job always produces the complete analysis.
func promoteGuestPreview(ctx context.Context, tx pgx.Tx, guestPrincipalID string, userPrincipalID string, now time.Time) (*GuestPreviewPromotion, error) {
	var preview struct {
		ID                 string
		AudioAssetID       string
		Topic              string
		Duration           int
		RecordingTimestamp time.Time
		PracticeType       string
		State              string
		Transcript         string
		PreviewJobID       string
	}
	err := tx.QueryRow(ctx, `
		SELECT id, audio_asset_id, topic, duration, recording_timestamp,
		       practice_type, state, transcript, preview_job_id
		FROM guest_previews
		WHERE guest_principal_id = $1 AND expires_at > $2
		FOR UPDATE`, guestPrincipalID, now).Scan(
		&preview.ID, &preview.AudioAssetID, &preview.Topic, &preview.Duration,
		&preview.RecordingTimestamp, &preview.PracticeType, &preview.State,
		&preview.Transcript, &preview.PreviewJobID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &GuestPreviewPromotion{Status: "no_preview"}, nil
	}
	if err != nil {
		return nil, err
	}
	if preview.State == "promoted" {
		return &GuestPreviewPromotion{Status: "promoted", PreviewID: preview.ID, RecordingID: preview.ID}, nil
	}

	// A ready preview already contains the server-verified duration. When auth
	// wins the race with the preview worker, reserve the full guest maximum so a
	// client-declared one-second duration cannot temporarily evade weekly quota.
	recordingDuration := preview.Duration
	if preview.State != "ready" {
		recordingDuration = 60
	}
	eligible, reason, err := lockGuestPreviewPromotionEligibility(ctx, tx, userPrincipalID, recordingDuration, now)
	if err != nil {
		return nil, err
	}
	if !eligible {
		if _, err := tx.Exec(ctx, `
			UPDATE processing_jobs
			SET state = 'cancelled', completed_at = $2, updated_at = $2,
			    lease_token = NULL, lease_owner = NULL, lease_expires_at = NULL,
			    heartbeat_at = NULL
			WHERE id = $1 AND kind = 'guest.preview'
			  AND state IN ('queued', 'retry_wait')`, preview.PreviewJobID, now); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE guest_previews
			SET state = 'failed', processing_error = $2, updated_at = $3
			WHERE id = $1 AND state <> 'promoted'`, preview.ID, "Guest preview was not promoted: "+reason, now); err != nil {
			return nil, err
		}
		return &GuestPreviewPromotion{Status: "not_promoted", PreviewID: preview.ID, Reason: reason}, nil
	}

	// A queued/retrying preview can be cancelled without racing a lease holder.
	// A running worker is fenced by the preview state transition below and must
	// not publish after state=promoted.
	if _, err := tx.Exec(ctx, `
		UPDATE processing_jobs
		SET state = 'cancelled', completed_at = $2, updated_at = $2,
		    lease_token = NULL, lease_owner = NULL, lease_expires_at = NULL,
		    heartbeat_at = NULL
		WHERE id = $1 AND kind = 'guest.preview'
		  AND state IN ('queued', 'retry_wait')`, preview.PreviewJobID, now); err != nil {
		return nil, err
	}

	transcript := ""
	processingStage := "transcribing"
	if preview.State == "ready" {
		transcript = preview.Transcript
		processingStage = "suggestions"
	}
	fullJobID := uuid.NewString()
	assetResult, err := tx.Exec(ctx, `
		UPDATE media_assets
		SET owner_principal_id = $3, purpose = 'recording_audio',
		    attached_at = COALESCE(attached_at, $4), retention_until = NULL,
		    updated_at = $4
		WHERE id = $1 AND owner_principal_id = $2
		  AND purpose = 'guest_preview_audio' AND state = 'ready' AND deleted_at IS NULL`,
		preview.AudioAssetID, guestPrincipalID, userPrincipalID, now)
	if err != nil {
		return nil, err
	}
	if assetResult.RowsAffected() != 1 {
		return nil, errors.New("guest preview audio is unavailable for promotion")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, corrected_transcript,
		   suggestions, practice_type, audio_data_url, photo_data_url, photo_object,
		   status, processing_stage, processing_job_id, audio_asset_id)
		VALUES
		  ($1, $2, $3, $4, $5, $6, '', '[]'::jsonb, $7, NULL, NULL, NULL,
		   'processing', $8, $9, $10)`,
		preview.ID, userPrincipalID, preview.Topic, recordingDuration,
		preview.RecordingTimestamp, transcript, preview.PracticeType,
		processingStage, fullJobID, preview.AudioAssetID,
	); err != nil {
		return nil, err
	}

	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
		ID:             fullJobID,
		Kind:           workqueue.KindRecordingProcess,
		ResourceID:     preview.ID,
		IdempotencyKey: "guest.preview.promote:" + preview.ID,
		MaxAttempts:    3,
		AvailableAt:    now,
	}); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO guest_preview_entitlements (user_id, preview_id, consumed_at)
		VALUES ($1, $2, $3)`, userPrincipalID, preview.ID, now); err != nil {
		return nil, err
	}
	result, err := tx.Exec(ctx, `
		UPDATE guest_previews
		SET state = 'promoted', promoted_recording_id = $2,
		    promoted_job_id = $3, promoted_at = $4, updated_at = $4,
		    processing_error = NULL
		WHERE id = $1 AND state <> 'promoted'`,
		preview.ID, preview.ID, fullJobID, now)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, errors.New("guest preview promotion lost its state transition")
	}
	return &GuestPreviewPromotion{Status: "promoted", PreviewID: preview.ID, RecordingID: preview.ID}, nil
}

func lockGuestPreviewPromotionEligibility(ctx context.Context, tx pgx.Tx, userID string, duration int, now time.Time) (bool, string, error) {
	recordingQuota, err := quota.LockRecordingQuota(ctx, tx, userID, now)
	if err != nil {
		return false, "", err
	}
	var entitlementConsumed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM guest_preview_entitlements WHERE user_id = $1)`, userID).Scan(&entitlementConsumed); err != nil {
		return false, "", err
	}
	if entitlementConsumed {
		return false, "promotion_already_used", nil
	}
	if recordingQuota.IsSubscriber {
		return true, "", nil
	}
	if recordingQuota.WeeklyRemainingSeconds == nil || duration > *recordingQuota.WeeklyRemainingSeconds {
		return false, "quota_exceeded", nil
	}
	return true, "", nil
}
