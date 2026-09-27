package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/practice"
	"daily-speaking-practice/backend/internal/recording"
)

func (s *Server) handleGetRecording(w http.ResponseWriter, r *http.Request, recordingID string) {
	user, ok := s.authorizedUser(w, r, "api.recordings.by-id.get")
	if !ok {
		return
	}
	record, err := s.recordingReader.Get(r.Context(), user.ID, recordingID)
	if errors.Is(err, recording.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load recording."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recording": recordingResponseFromRecord(record)})
}

// recordingForUser remains a delivery helper for handlers that still return
// the legacy web response shape. Persistence is owned by recording.Reader.
func (s *Server) recordingForUser(ctx context.Context, userID string, recordingID string) (recordingResponse, error) {
	record, err := s.recordingReader.Get(ctx, userID, recordingID)
	if err != nil {
		return recordingResponse{}, err
	}
	return recordingResponseFromRecord(record), nil
}

func recordingResponseFromRecord(record recording.Record) recordingResponse {
	return recordingResponse{
		ID:                  record.ID,
		Topic:               record.Topic,
		Duration:            recording.NormalizeDurationSeconds(record.Duration),
		Timestamp:           record.Timestamp.UTC().Format(time.RFC3339Nano),
		Status:              normalizeRecordingStatus(record.Status),
		Transcript:          record.Transcript,
		CorrectedTranscript: record.CorrectedTranscript,
		Suggestions:         normalizeSuggestions(record.SuggestionsJSON, 0),
		ProcessingStage:     normalizeRecordingProcessingStage(record.ProcessingStage),
		PracticeType:        practice.NormalizeType(record.PracticeType),
		AudioDataURL:        normalizeOptionalAudio(record.AudioDataURL, true),
		PhotoDataURL:        normalizeOptionalPhoto(record.PhotoDataURL),
		PhotoObject:         normalizeOptionalPhotoObject(record.PhotoObject),
		ProcessingError:     normalizeOptionalProcessingError(record.ProcessingError),
		ShadowingStatus:     normalizeShadowingStatus(record.ShadowingStatus),
		ShadowingAudioURL:   normalizeOptionalShadowingAudio(record.ShadowingAudioURL),
		ShadowingError:      normalizeOptionalProcessingError(record.ShadowingError),
		ShadowingUpdatedAt:  record.ShadowingUpdatedAt.UTC().Format(time.RFC3339Nano),
		Media:               recordingMedia(record.AudioAssetID, record.PhotoAssetID, record.ShadowingAssetID),
	}
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
	return media.NormalizeStoredShadowingAudioSource(*value)
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
