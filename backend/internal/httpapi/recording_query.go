package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Server) handleGetRecording(w http.ResponseWriter, r *http.Request, recordingID string) {
	user, ok := s.authorizedUser(w, r, "api.recordings.by-id.get")
	if !ok {
		return
	}
	recording, err := s.recordingForUser(r.Context(), user.ID, recordingID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load recording."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recording": recording})
}

func (s *Server) recordingForUser(ctx context.Context, userID string, recordingID string) (recordingResponse, error) {
	var row struct {
		ID                  string
		Topic               string
		Duration            int
		Timestamp           time.Time
		Status              string
		Transcript          string
		CorrectedTranscript string
		Suggestions         []byte
		ProcessingStage     *string
		PracticeType        string
		AudioDataURL        *string
		PhotoDataURL        *string
		PhotoObject         *string
		ProcessingError     *string
		ShadowingStatus     string
		ShadowingAudioURL   *string
		ShadowingError      *string
		ShadowingUpdatedAt  time.Time
		AudioAssetID        *string
		PhotoAssetID        *string
		ShadowingAssetID    *string
	}
	err := s.db.QueryRow(ctx, `
		SELECT id, topic, duration, timestamp, status, transcript, corrected_transcript, suggestions,
		       processing_stage, practice_type, audio_data_url, photo_data_url, photo_object, processing_error,
		       shadowing_status, shadowing_audio_url, shadowing_error, shadowing_updated_at,
		       audio_asset_id, photo_asset_id, shadowing_asset_id
		FROM recordings
		WHERE id = $1 AND user_id = $2
		LIMIT 1`, strings.TrimSpace(recordingID), userID).Scan(&row.ID, &row.Topic, &row.Duration, &row.Timestamp, &row.Status, &row.Transcript, &row.CorrectedTranscript, &row.Suggestions, &row.ProcessingStage, &row.PracticeType, &row.AudioDataURL, &row.PhotoDataURL, &row.PhotoObject, &row.ProcessingError, &row.ShadowingStatus, &row.ShadowingAudioURL, &row.ShadowingError, &row.ShadowingUpdatedAt, &row.AudioAssetID, &row.PhotoAssetID, &row.ShadowingAssetID)
	if err != nil {
		return recordingResponse{}, err
	}
	return recordingResponse{
		ID:                  row.ID,
		Topic:               row.Topic,
		Duration:            domain.ToNonNegativeInt(row.Duration),
		Timestamp:           row.Timestamp.UTC().Format(time.RFC3339Nano),
		Status:              normalizeRecordingStatus(row.Status),
		Transcript:          row.Transcript,
		CorrectedTranscript: row.CorrectedTranscript,
		Suggestions:         normalizeSuggestions(row.Suggestions, 0),
		ProcessingStage:     normalizeRecordingProcessingStage(row.ProcessingStage),
		PracticeType:        domain.NormalizePracticeType(row.PracticeType),
		AudioDataURL:        normalizeOptionalAudio(row.AudioDataURL, true),
		PhotoDataURL:        normalizeOptionalPhoto(row.PhotoDataURL),
		PhotoObject:         normalizeOptionalPhotoObject(row.PhotoObject),
		ProcessingError:     normalizeOptionalProcessingError(row.ProcessingError),
		ShadowingStatus:     normalizeShadowingStatus(row.ShadowingStatus),
		ShadowingAudioURL:   normalizeOptionalShadowingAudio(row.ShadowingAudioURL),
		ShadowingError:      normalizeOptionalProcessingError(row.ShadowingError),
		ShadowingUpdatedAt:  row.ShadowingUpdatedAt.UTC().Format(time.RFC3339Nano),
		Media:               recordingMedia(row.AudioAssetID, row.PhotoAssetID, row.ShadowingAssetID),
	}, nil
}

func normalizeRecordingStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "processing", "ready", "failed":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "ready"
	}
}

func normalizeRecordingProcessingStage(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	switch normalized {
	case "transcribing", "suggestions", "rewriting":
		return &normalized
	default:
		return nil
	}
}

func normalizeShadowingStatus(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "pending", "processing", "ready", "failed":
		return normalized
	default:
		return "pending"
	}
}

func normalizeOptionalShadowingAudio(value *string) *string {
	if value == nil {
		return nil
	}
	return domain.NormalizeStoredShadowingAudioSource(*value)
}

func normalizeOptionalProcessingError(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}
