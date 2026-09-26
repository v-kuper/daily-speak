package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxRecordingCreateV1IdempotencyKeyBytes = 200

var (
	errRecordingCreateV1MediaNotFound       = errors.New("recording media not found")
	errRecordingCreateV1IdempotencyConflict = errors.New("idempotency key was already used with a different request")
)

type recordingCreateV1Request struct {
	Topic        string  `json:"topic"`
	Duration     int     `json:"duration"`
	Timestamp    string  `json:"timestamp"`
	PracticeType string  `json:"practiceType"`
	AudioAssetID string  `json:"audioAssetId"`
	PhotoAssetID *string `json:"photoAssetId"`
}

type normalizedRecordingCreateV1 struct {
	Topic        string
	Duration     int
	Timestamp    time.Time
	PracticeType string
	AudioAssetID string
	PhotoAssetID *string
}

type recordingCreateV1Row struct {
	Response      recordingResponse
	Timestamp     time.Time
	Suggestions   []byte
	ShadowingTime time.Time
	AudioAssetID  string
	PhotoAssetID  *string
}

func (s *Server) handleCreateRecordingV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return
	}
	if identity.Kind != "user" || identity.User == nil {
		writeV1Error(w, r, http.StatusForbidden, "account_required", "An account is required to process a recording")
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" || len(idempotencyKey) > maxRecordingCreateV1IdempotencyKeyBytes {
		writeV1Error(w, r, http.StatusBadRequest, "idempotency_key_required", "A valid Idempotency-Key header is required")
		return
	}

	var payload recordingCreateV1Request
	if !decodeIdentityJSON(w, r, &payload) {
		return
	}
	normalized, err := normalizeRecordingCreateV1(payload)
	if err != nil {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	recording, recordingQuota, err := s.createRecordingFromAssetsV1(r.Context(), identity, idempotencyKey, normalized)
	var quotaError *quotaHTTPError
	switch {
	case errors.Is(err, errRecordingCreateV1MediaNotFound):
		writeV1Error(w, r, http.StatusNotFound, "media_not_found", "Ready media owned by this account was not found")
	case errors.Is(err, errRecordingCreateV1IdempotencyConflict):
		writeV1Error(w, r, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used with a different request")
	case errors.As(err, &quotaError):
		writeV1Error(w, r, quotaError.status, "quota_exceeded", quotaError.message)
	case err != nil:
		writeV1Error(w, r, http.StatusInternalServerError, "internal_error", "Failed to create recording")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"recording": recording, "quota": recordingQuota})
	}
}

func (e *quotaHTTPError) Error() string {
	return e.message
}

func normalizeRecordingCreateV1(payload recordingCreateV1Request) (normalizedRecordingCreateV1, error) {
	topic := truncateRunes(strings.TrimSpace(payload.Topic), 300)
	if topic == "" {
		return normalizedRecordingCreateV1{}, errors.New("Recording topic is required")
	}
	if payload.Duration <= 0 {
		return normalizedRecordingCreateV1{}, errors.New("Recording duration must be positive")
	}
	practiceType := strings.ToLower(strings.TrimSpace(payload.PracticeType))
	switch practiceType {
	case "free_talk", "topic", "photo_description":
	default:
		return normalizedRecordingCreateV1{}, errors.New("Practice type is invalid")
	}
	timestampText := strings.TrimSpace(payload.Timestamp)
	timestamp, err := time.Parse(time.RFC3339Nano, timestampText)
	if err != nil || timestampText == "" {
		return normalizedRecordingCreateV1{}, errors.New("Recording timestamp must be an RFC3339 value")
	}
	audioAssetID := strings.TrimSpace(payload.AudioAssetID)
	if audioAssetID == "" {
		return normalizedRecordingCreateV1{}, errors.New("Audio asset is required")
	}
	var photoAssetID *string
	if payload.PhotoAssetID != nil {
		value := strings.TrimSpace(*payload.PhotoAssetID)
		if value != "" {
			photoAssetID = &value
		}
	}
	if practiceType == "photo_description" && photoAssetID == nil {
		return normalizedRecordingCreateV1{}, errors.New("Photo asset is required for photo description practice")
	}
	return normalizedRecordingCreateV1{
		Topic:        topic,
		Duration:     payload.Duration,
		Timestamp:    timestamp.UTC().Truncate(time.Microsecond),
		PracticeType: practiceType,
		AudioAssetID: audioAssetID,
		PhotoAssetID: photoAssetID,
	}, nil
}

func (s *Server) createRecordingFromAssetsV1(ctx context.Context, identity *auth.Identity, idempotencyKey string, input normalizedRecordingCreateV1) (recordingResponse, quota.RecordingQuota, error) {
	recordingID, digest := deterministicRecordingCreateV1Identity(identity.PrincipalID, idempotencyKey, "recording")
	jobID, _ := deterministicRecordingCreateV1Identity(identity.PrincipalID, idempotencyKey, "recording-job")
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return recordingResponse{}, quota.RecordingQuota{}, err
	}
	defer tx.Rollback(ctx)

	q, err := lockRecordingQuotaV1(ctx, tx, identity.User.ID)
	if err != nil {
		return recordingResponse{}, quota.RecordingQuota{}, err
	}
	existing, found, err := recordingCreateV1ByID(ctx, tx, identity.User.ID, recordingID)
	if err != nil {
		return recordingResponse{}, quota.RecordingQuota{}, err
	}
	if found {
		if !recordingCreateV1Matches(existing, input) {
			return recordingResponse{}, quota.RecordingQuota{}, errRecordingCreateV1IdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return recordingResponse{}, quota.RecordingQuota{}, err
		}
		return existing.Response, q, nil
	}
	if quotaError := recordingQuotaError(q, input.Duration); quotaError != nil {
		return recordingResponse{}, quota.RecordingQuota{}, quotaError
	}
	if err := lockReadyMediaAssetV1(ctx, tx, identity.PrincipalID, input.AudioAssetID, "recording_audio"); err != nil {
		return recordingResponse{}, quota.RecordingQuota{}, err
	}
	if input.PhotoAssetID != nil {
		if err := lockReadyMediaAssetV1(ctx, tx, identity.PrincipalID, *input.PhotoAssetID, "recording_photo"); err != nil {
			return recordingResponse{}, quota.RecordingQuota{}, err
		}
	}

	row, err := insertRecordingFromAssetsV1(ctx, tx, identity.User.ID, recordingID, jobID, input)
	if err != nil {
		return recordingResponse{}, quota.RecordingQuota{}, err
	}
	assetIDs := []string{input.AudioAssetID}
	if input.PhotoAssetID != nil {
		assetIDs = append(assetIDs, *input.PhotoAssetID)
	}
	result, err := tx.Exec(ctx, `
		UPDATE media_assets
		SET attached_at = NOW(), retention_until = NULL, updated_at = NOW()
		WHERE id = ANY($1::text[]) AND owner_principal_id = $2
		  AND state = 'ready' AND attached_at IS NULL`, assetIDs, identity.PrincipalID)
	if err != nil {
		return recordingResponse{}, quota.RecordingQuota{}, err
	}
	if result.RowsAffected() != int64(len(assetIDs)) {
		return recordingResponse{}, quota.RecordingQuota{}, errRecordingCreateV1MediaNotFound
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
		ID:             jobID,
		Kind:           workqueue.KindRecordingProcess,
		ResourceID:     recordingID,
		IdempotencyKey: "recording.create:" + digest,
		MaxAttempts:    3,
	}); err != nil {
		return recordingResponse{}, quota.RecordingQuota{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordingResponse{}, quota.RecordingQuota{}, err
	}
	return row.Response, recordingQuotaAfterSave(q, input.Duration), nil
}

func deterministicRecordingCreateV1Identity(principalID string, idempotencyKey string, scope string) (string, string) {
	digestBytes := sha256.Sum256([]byte(scope + "\x1f" + principalID + "\x1f" + idempotencyKey))
	digest := hex.EncodeToString(digestBytes[:])
	var id uuid.UUID
	copy(id[:], digestBytes[:16])
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String(), digest
}

func lockRecordingQuotaV1(ctx context.Context, tx pgx.Tx, userID string) (quota.RecordingQuota, error) {
	var isSubscriber bool
	if err := tx.QueryRow(ctx, `
		SELECT is_subscriber AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())
		FROM users
		WHERE id = $1
		FOR UPDATE`, userID).Scan(&isSubscriber); err != nil {
		return quota.RecordingQuota{}, err
	}
	var usedSeconds int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(duration), 0)::int
		FROM recordings
		WHERE user_id = $1
		  AND created_at >= date_trunc('week', NOW())
		  AND created_at < date_trunc('week', NOW()) + INTERVAL '1 week'`, userID).Scan(&usedSeconds); err != nil {
		return quota.RecordingQuota{}, err
	}
	usedSeconds = domain.ToNonNegativeInt(usedSeconds)
	if isSubscriber {
		return quota.RecordingQuota{IsSubscriber: true, WeeklyUsedSeconds: usedSeconds, MaxSessionSeconds: domain.SubscriberMaxSessionSeconds}, nil
	}
	limit := domain.FreeWeeklyLimitSeconds
	remaining := limit - usedSeconds
	if remaining < 0 {
		remaining = 0
	}
	return quota.RecordingQuota{
		WeeklyLimitSeconds:     &limit,
		WeeklyUsedSeconds:      usedSeconds,
		WeeklyRemainingSeconds: &remaining,
		MaxSessionSeconds:      domain.SubscriberMaxSessionSeconds,
	}, nil
}

func lockReadyMediaAssetV1(ctx context.Context, tx pgx.Tx, principalID string, assetID string, purpose string) error {
	var found string
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM media_assets
		WHERE id = $1 AND owner_principal_id = $2 AND purpose = $3
		  AND state = 'ready' AND attached_at IS NULL AND deleted_at IS NULL
		FOR UPDATE`, assetID, principalID, purpose).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return errRecordingCreateV1MediaNotFound
	}
	return err
}

func insertRecordingFromAssetsV1(ctx context.Context, tx pgx.Tx, userID string, recordingID string, jobID string, input normalizedRecordingCreateV1) (recordingCreateV1Row, error) {
	var row recordingCreateV1Row
	err := tx.QueryRow(ctx, `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, corrected_transcript,
		   suggestions, practice_type, audio_data_url, photo_data_url, photo_object,
		   status, processing_stage, processing_job_id, audio_asset_id, photo_asset_id)
		VALUES
		  ($1, $2, $3, $4, $5, '', '', '[]'::jsonb, $6, NULL, NULL, NULL,
		   'processing', 'transcribing', $7, $8, $9)
		RETURNING id, topic, duration, timestamp, status, transcript,
		          corrected_transcript, suggestions, processing_stage, practice_type,
		          audio_data_url, photo_data_url, photo_object, processing_error,
		          shadowing_status, shadowing_audio_url, shadowing_error,
		          shadowing_updated_at, audio_asset_id, photo_asset_id`,
		recordingID, userID, input.Topic, input.Duration, input.Timestamp,
		input.PracticeType, jobID, input.AudioAssetID, input.PhotoAssetID,
	).Scan(
		&row.Response.ID, &row.Response.Topic, &row.Response.Duration, &row.Timestamp,
		&row.Response.Status, &row.Response.Transcript, &row.Response.CorrectedTranscript,
		&row.Suggestions, &row.Response.ProcessingStage, &row.Response.PracticeType,
		&row.Response.AudioDataURL, &row.Response.PhotoDataURL, &row.Response.PhotoObject,
		&row.Response.ProcessingError, &row.Response.ShadowingStatus,
		&row.Response.ShadowingAudioURL, &row.Response.ShadowingError,
		&row.ShadowingTime, &row.AudioAssetID, &row.PhotoAssetID,
	)
	if err != nil {
		return recordingCreateV1Row{}, err
	}
	normalizeRecordingCreateV1Row(&row)
	return row, nil
}

func recordingCreateV1ByID(ctx context.Context, tx pgx.Tx, userID string, recordingID string) (recordingCreateV1Row, bool, error) {
	var row recordingCreateV1Row
	err := tx.QueryRow(ctx, `
		SELECT id, topic, duration, timestamp, status, transcript,
		       corrected_transcript, suggestions, processing_stage, practice_type,
		       audio_data_url, photo_data_url, photo_object, processing_error,
		       shadowing_status, shadowing_audio_url, shadowing_error,
		       shadowing_updated_at, audio_asset_id, photo_asset_id
		FROM recordings
		WHERE id = $1 AND user_id = $2`, recordingID, userID).Scan(
		&row.Response.ID, &row.Response.Topic, &row.Response.Duration, &row.Timestamp,
		&row.Response.Status, &row.Response.Transcript, &row.Response.CorrectedTranscript,
		&row.Suggestions, &row.Response.ProcessingStage, &row.Response.PracticeType,
		&row.Response.AudioDataURL, &row.Response.PhotoDataURL, &row.Response.PhotoObject,
		&row.Response.ProcessingError, &row.Response.ShadowingStatus,
		&row.Response.ShadowingAudioURL, &row.Response.ShadowingError,
		&row.ShadowingTime, &row.AudioAssetID, &row.PhotoAssetID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordingCreateV1Row{}, false, nil
	}
	if err != nil {
		return recordingCreateV1Row{}, false, err
	}
	normalizeRecordingCreateV1Row(&row)
	return row, true, nil
}

func normalizeRecordingCreateV1Row(row *recordingCreateV1Row) {
	row.Response.Duration = domain.ToNonNegativeInt(row.Response.Duration)
	row.Response.Timestamp = row.Timestamp.UTC().Format(time.RFC3339Nano)
	row.Response.Status = normalizeRecordingStatus(row.Response.Status)
	row.Response.Suggestions = normalizeSuggestions(row.Suggestions, 0)
	row.Response.ProcessingStage = normalizeRecordingProcessingStage(row.Response.ProcessingStage)
	row.Response.PracticeType = domain.NormalizePracticeType(row.Response.PracticeType)
	row.Response.AudioDataURL = normalizeOptionalAudio(row.Response.AudioDataURL, true)
	row.Response.PhotoDataURL = normalizeOptionalPhoto(row.Response.PhotoDataURL)
	row.Response.PhotoObject = normalizeOptionalPhotoObject(row.Response.PhotoObject)
	row.Response.ProcessingError = normalizeOptionalProcessingError(row.Response.ProcessingError)
	row.Response.ShadowingStatus = normalizeShadowingStatus(row.Response.ShadowingStatus)
	row.Response.ShadowingAudioURL = normalizeOptionalShadowingAudio(row.Response.ShadowingAudioURL)
	row.Response.ShadowingError = normalizeOptionalProcessingError(row.Response.ShadowingError)
	row.Response.ShadowingUpdatedAt = row.ShadowingTime.UTC().Format(time.RFC3339Nano)
	row.Response.Media = recordingMedia(&row.AudioAssetID, row.PhotoAssetID, nil)
}

func recordingCreateV1Matches(row recordingCreateV1Row, input normalizedRecordingCreateV1) bool {
	return row.Response.Topic == input.Topic &&
		row.Response.Duration == input.Duration &&
		row.Timestamp.Equal(input.Timestamp) &&
		row.Response.PracticeType == input.PracticeType &&
		row.AudioAssetID == input.AudioAssetID &&
		equalOptionalString(row.PhotoAssetID, input.PhotoAssetID)
}

func equalOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
