package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/practice"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/recording"
)

const maxRecordingCreateV1IdempotencyKeyBytes = 200

type recordingCreateV1Request struct {
	Topic              string  `json:"topic"`
	Duration           int     `json:"duration"`
	Timestamp          string  `json:"timestamp"`
	PracticeType       string  `json:"practiceType"`
	AudioAssetID       string  `json:"audioAssetId"`
	PhotoAssetID       *string `json:"photoAssetId"`
	PhotoObject        *string `json:"photoObject"`
	InterviewSessionID *string `json:"interviewSessionId,omitempty"`
}

func (s *Server) handleCreateRecordingV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
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
	input, err := parseRecordingCreateV1(payload)
	if err != nil {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	created, currentQuota, err := s.recordingCreator.Create(
		r.Context(), identity.PrincipalID, identity.User.ID, idempotencyKey, input,
	)
	var quotaViolation *recording.QuotaViolation
	var validationError *recording.ValidationError
	switch {
	case errors.As(err, &validationError):
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", validationError.Message)
	case errors.Is(err, recording.ErrCreateMediaNotFound):
		writeV1Error(w, r, http.StatusNotFound, "media_not_found", "Ready media owned by this account was not found")
	case errors.Is(err, recording.ErrCreateIdempotencyConflict):
		writeV1Error(w, r, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used with a different request")
	case errors.Is(err, recording.ErrInterviewSessionUnavailable):
		writeV1Error(w, r, http.StatusConflict, "interview_session_conflict", "Interview session is unavailable")
	case errors.As(err, &quotaViolation):
		maximum := quotaViolation.MaxSessionSeconds
		if maximum <= 0 {
			maximum = quota.AccountMaxSessionSeconds
		}
		writeV1Error(w, r, http.StatusBadRequest, "quota_exceeded",
			"Accounts can save recordings up to "+quota.FormatSeconds(maximum)+" per recording.")
	case err != nil:
		writeV1Error(w, r, http.StatusInternalServerError, "internal_error", "Failed to create recording")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{
			"recording": recordingV1ResponseFromCreated(created),
			"quota":     currentQuota,
		})
	}
}

func parseRecordingCreateV1(payload recordingCreateV1Request) (recording.CreateInput, error) {
	timestampText := strings.TrimSpace(payload.Timestamp)
	timestamp, err := time.Parse(time.RFC3339Nano, timestampText)
	if err != nil || timestampText == "" {
		return recording.CreateInput{}, errors.New("Recording timestamp must be an RFC3339 value")
	}
	return recording.CreateInput{
		Topic: payload.Topic, Duration: payload.Duration, Timestamp: timestamp,
		PracticeType: payload.PracticeType, AudioAssetID: payload.AudioAssetID,
		PhotoAssetID: payload.PhotoAssetID, PhotoObject: payload.PhotoObject,
		InterviewSessionID: payload.InterviewSessionID,
	}, nil
}

func recordingV1ResponseFromCreated(created recording.Created) recordingV1Response {
	audioAssetID := created.AudioAssetID
	return recordingV1Response{
		ID: created.ID, Topic: created.Topic, Duration: recording.NormalizeDurationSeconds(created.Duration),
		Timestamp: created.Timestamp.UTC().Format(time.RFC3339Nano), Status: normalizeRecordingStatus(created.Status),
		Transcript: created.Transcript, CorrectedTranscript: created.CorrectedTranscript,
		Suggestions:        normalizeSuggestions(created.SuggestionsJSON, 0),
		ProcessingStage:    normalizeRecordingProcessingStage(created.ProcessingStage),
		PracticeType:       practice.NormalizeType(created.PracticeType),
		PhotoObject:        normalizeOptionalPhotoObject(created.PhotoObject),
		ProcessingError:    normalizeOptionalProcessingError(created.ProcessingError),
		ShadowingStatus:    normalizeShadowingStatus(created.ShadowingStatus),
		ShadowingError:     normalizeOptionalProcessingError(created.ShadowingError),
		ShadowingUpdatedAt: created.ShadowingUpdatedAt.UTC().Format(time.RFC3339Nano),
		Media:              recordingMedia(&audioAssetID, created.PhotoAssetID, nil),
	}
}
