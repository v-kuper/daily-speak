package httpapi

import (
	"context"
	"daily-speaking-practice/backend/internal/interview"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/practice"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/shadowing"
)

// recordingV1Response exposes persisted private media only through owned media
// references that clients exchange for short-lived download requests.
type recordingV1Response struct {
	FocusedFeedback     *recording.FocusedFeedback `json:"focusedFeedback,omitempty"`
	ShadowingScript     *shadowing.Script          `json:"shadowingScript,omitempty"`
	ID                  string                     `json:"id"`
	Topic               string                     `json:"topic"`
	Duration            int                        `json:"duration"`
	Timestamp           string                     `json:"timestamp"`
	Status              string                     `json:"status"`
	Transcript          string                     `json:"transcript"`
	CorrectedTranscript string                     `json:"correctedTranscript"`
	Suggestions         []suggestion               `json:"suggestions"`
	Strengths           []strength                 `json:"strengths"`
	StrengthsStatus     string                     `json:"strengthsStatus,omitempty"`
	ProcessingStage     *string                    `json:"processingStage"`
	PracticeType        string                     `json:"practiceType"`
	PhotoObject         *string                    `json:"photoObject"`
	ProcessingError     *string                    `json:"processingError"`
	ShadowingStatus     string                     `json:"shadowingStatus"`
	ShadowingError      *string                    `json:"shadowingError"`
	ShadowingUpdatedAt  string                     `json:"shadowingUpdatedAt"`
	Media               *recordingMediaResponse    `json:"media,omitempty"`
	InterviewTurns      []recording.InterviewTurn  `json:"interviewTurns,omitempty"`
}

func (s *Server) routeRecordingV1(w http.ResponseWriter, r *http.Request, relativePath string) {
	parts := strings.Split(strings.Trim(relativePath, "/"), "/")
	if len(parts) == 3 && parts[1] == "feedback" && parts[2] == "reanalyze" {
		s.handleFeedbackReanalysisV1(w, r, parts[0])
		return
	}
	if len(parts) == 4 && parts[1] == "feedback" && parts[3] == "audio" {
		s.handleFeedbackAudioV1(w, r, parts[0], "", parts[2])
		return
	}
	if len(parts) >= 4 && parts[1] == "interview-turns" {
		seq, err := strconv.Atoi(parts[2])
		if err != nil || seq < 1 {
			s.writeInterviewError(w, r, interview.ErrInvalid)
			return
		}
		if len(parts) == 4 && parts[3] == "question-audio" {
			s.handleQuestionAudioV1(w, r, interview.QuestionAudioInput{RecordingID: parts[0], TurnSeq: seq})
			return
		}
		if parts[3] == "attempts" {
			if len(parts) == 4 {
				s.handleAnswerAttemptsV1(w, r, parts[0], seq, "")
				return
			}
			if len(parts) == 6 && parts[5] == "retry" {
				s.handleRetryAttemptV1(w, r, parts[0], seq, parts[4])
				return
			}
			if len(parts) == 5 {
				s.handleAnswerAttemptsV1(w, r, parts[0], seq, parts[4])
				return
			}
			if len(parts) == 8 && parts[5] == "feedback" && parts[7] == "audio" {
				s.handleFeedbackAudioV1(w, r, parts[0], parts[4], parts[6])
				return
			}
		}
	}
	if len(parts) == 1 && parts[0] != "" {
		recordingID := pathUnescape(parts[0])
		switch r.Method {
		case http.MethodGet:
			s.handleGetRecordingV1(w, r, recordingID)
		case http.MethodDelete:
			s.handleDeleteRecordingV1(w, r, recordingID)
		default:
			writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[0] != "" && r.Method == http.MethodPost {
		recordingID := pathUnescape(parts[0])
		switch parts[1] {
		case "retry":
			s.handleRetryRecordingV1(w, r, recordingID)
		case "shadowing":
			s.handleGenerateShadowingV1(w, r, recordingID)
		case "strengths":
			s.handleRetryStrengthsV1(w, r, recordingID)
		default:
			writeV1Error(w, r, http.StatusNotFound, "not_found", "Not found")
		}
		return
	}
	if len(parts) == 2 && parts[0] != "" && (parts[1] == "retry" || parts[1] == "shadowing" || parts[1] == "strengths") {
		writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	writeV1Error(w, r, http.StatusNotFound, "not_found", "Not found")
}

func (s *Server) handleGetRecordingV1(w http.ResponseWriter, r *http.Request, recordingID string) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	record, err := s.recordingReader.Get(r.Context(), identity.User.ID, recordingID)
	if errors.Is(err, recording.ErrNotFound) {
		writeV1Error(w, r, http.StatusNotFound, "recording_not_found", "Recording not found")
		return
	}
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "recording_unavailable", "Failed to load recording")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recording": recordingV1ResponseFromRecord(record)})
}

func (s *Server) handleRetryRecordingV1(w http.ResponseWriter, r *http.Request, recordingID string) {
	started := time.Now()
	logger := logging.ForRequest("api.v1.recordings.retry", r)
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_recording_id", "Recording ID is required")
		return
	}

	result, err := s.recordingRetryService.Retry(r.Context(), identity.User.ID, recordingID)
	if errors.Is(err, recording.ErrNotFound) {
		writeV1Error(w, r, http.StatusNotFound, "recording_not_found", "Recording not found")
		return
	}
	if errors.Is(err, recording.ErrRetryUnavailable) {
		writeV1Error(w, r, http.StatusConflict, "recording_retry_unavailable", recording.ErrRetryUnavailable.Error())
		return
	}
	if err != nil {
		logger.Error("recording.retry_schedule_failed", map[string]any{"recordingId": recordingID})
		writeV1Error(w, r, http.StatusInternalServerError, "recording_unavailable", "Failed to retry recording processing")
		return
	}
	logger.Info("request.success", map[string]any{
		"status":      http.StatusOK,
		"durationMs":  logging.ElapsedMs(started),
		"recordingId": recordingID,
		"scheduled":   result.Scheduled,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"recording": recordingV1ResponseFromRecord(result.Record),
		"scheduled": result.Scheduled,
	})
}

func (s *Server) handleGenerateShadowingV1(w http.ResponseWriter, r *http.Request, recordingID string) {
	started := time.Now()
	logger := logging.ForRequest("api.v1.recordings.shadowing", r)
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_recording_id", "Recording ID is required")
		return
	}
	record, scheduled, err := s.scheduleShadowingRecord(r.Context(), identity.User.ID, recordingID)
	if errors.Is(err, shadowing.ErrNotFound) || errors.Is(err, recording.ErrNotFound) {
		writeV1Error(w, r, http.StatusNotFound, "recording_not_found", "Recording not found")
		return
	}
	if errors.Is(err, shadowing.ErrTranscriptUnavailable) {
		writeV1Error(w, r, http.StatusConflict, "shadowing_not_ready", "The natural transcript is not ready yet")
		return
	}
	if err != nil {
		logger.Error("shadowing.schedule_failed", map[string]any{"recordingId": recordingID})
		writeV1Error(w, r, http.StatusInternalServerError, "shadowing_unavailable", "Failed to generate pronunciation audio")
		return
	}
	logger.Info("request.success", map[string]any{
		"status": http.StatusOK, "durationMs": logging.ElapsedMs(started),
		"recordingId": recordingID, "scheduled": scheduled,
	})
	writeJSON(w, http.StatusOK, map[string]any{"recording": recordingV1ResponseFromRecord(record)})
}

func (s *Server) handleListRecordingsV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_pagination", "Invalid pagination parameters")
		return
	}
	options := recording.ListOptions{Limit: page.Limit + 1}
	if page.Cursor != nil {
		options.BeforeTimestamp = &page.Cursor.Timestamp
		options.BeforeID = page.Cursor.ID
	}
	records, err := s.recordingReader.List(r.Context(), identity.User.ID, options)
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "recording_unavailable", "Failed to load recordings")
		return
	}
	var nextCursor *string
	if len(records) > page.Limit {
		last := records[page.Limit-1]
		encoded, err := encodePageCursor(pageCursor{Timestamp: last.Timestamp, ID: last.ID})
		if err != nil {
			writeV1Error(w, r, http.StatusInternalServerError, "recording_unavailable", "Failed to paginate recordings")
			return
		}
		nextCursor = &encoded
		records = records[:page.Limit]
	}
	responses := make([]recordingV1Response, 0, len(records))
	for _, record := range records {
		responses = append(responses, recordingV1ResponseFromRecord(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": responses,
		"page":  pageInfo{Limit: page.Limit, NextCursor: nextCursor},
	})
}

func recordingV1ResponseFromRecord(record recording.Record) recordingV1Response {
	return recordingV1Response{
		FocusedFeedback:     record.FocusedFeedback(),
		ShadowingScript:     shadowing.DecodeScript(record.ShadowingScriptJSON),
		ID:                  record.ID,
		Topic:               record.Topic,
		Duration:            recording.NormalizeDurationSeconds(record.Duration),
		Timestamp:           record.Timestamp.UTC().Format(time.RFC3339Nano),
		Status:              normalizeRecordingStatus(record.Status),
		Transcript:          record.Transcript,
		CorrectedTranscript: record.CorrectedTranscript,
		Suggestions:         normalizeSuggestions(record.SuggestionsJSON, 0),
		Strengths:           normalizeStrengths(record.StrengthsJSON, 3),
		StrengthsStatus:     record.StrengthsStatus,
		ProcessingStage:     normalizeRecordingProcessingStage(record.ProcessingStage),
		PracticeType:        practice.NormalizeType(record.PracticeType),
		PhotoObject:         normalizeOptionalPhotoObject(record.PhotoObject),
		ProcessingError:     normalizeOptionalProcessingError(record.ProcessingError),
		ShadowingStatus:     normalizeShadowingStatus(record.ShadowingStatus),
		ShadowingError:      normalizeOptionalProcessingError(record.ShadowingError),
		ShadowingUpdatedAt:  record.ShadowingUpdatedAt.UTC().Format(time.RFC3339Nano),
		Media:               recordingMedia(record.AudioAssetID, record.PhotoAssetID, record.ShadowingAssetID),
		InterviewTurns:      record.InterviewTurns,
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

func (s *Server) scheduleShadowingRecord(ctx context.Context, userID, recordingID string) (recording.Record, bool, error) {
	scheduled, err := s.shadowingStore.Schedule(ctx, userID, recordingID)
	if err != nil {
		return recording.Record{}, false, err
	}
	record, err := s.recordingReader.Get(ctx, userID, recordingID)
	return record, scheduled, err
}
