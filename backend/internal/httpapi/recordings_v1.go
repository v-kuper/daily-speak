package httpapi

import (
	"net/http"
	"time"

	"daily-speaking-practice/backend/internal/domain"
)

type recordingPageItem struct {
	recording recordingResponse
	timestamp time.Time
}

func (s *Server) handleListRecordingsV1(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.v1.recordings.list")
	if !ok {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid pagination parameters."})
		return
	}
	var cursorTime any
	var cursorID string
	if page.Cursor != nil {
		cursorTime = page.Cursor.Timestamp
		cursorID = page.Cursor.ID
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT
		  id, topic, duration, timestamp, transcript, corrected_transcript, suggestions,
		  practice_type, audio_data_url, photo_data_url, photo_object,
		  status, processing_stage, processing_error,
		  shadowing_status, shadowing_audio_url, shadowing_error, shadowing_updated_at
		FROM recordings
		WHERE user_id = $1
		  AND ($2::timestamptz IS NULL OR (timestamp, id) < ($2::timestamptz, $3::text))
		ORDER BY timestamp DESC, id DESC
		LIMIT $4`, user.ID, cursorTime, cursorID, page.Limit+1)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load recordings."})
		return
	}
	defer rows.Close()
	items := make([]recordingPageItem, 0, page.Limit+1)
	for rows.Next() {
		item, err := scanRecordingPageItem(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load recordings."})
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load recordings."})
		return
	}
	var nextCursor *string
	if len(items) > page.Limit {
		last := items[page.Limit-1]
		encoded, err := encodePageCursor(pageCursor{Timestamp: last.timestamp, ID: last.recording.ID})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to paginate recordings."})
			return
		}
		nextCursor = &encoded
		items = items[:page.Limit]
	}
	responses := make([]recordingResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, item.recording)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": responses,
		"page":  pageInfo{Limit: page.Limit, NextCursor: nextCursor},
	})
}

func scanRecordingPageItem(row scanner) (recordingPageItem, error) {
	var response recordingResponse
	var timestamp, shadowingUpdatedAt time.Time
	var suggestionsBytes []byte
	err := row.Scan(
		&response.ID, &response.Topic, &response.Duration, &timestamp,
		&response.Transcript, &response.CorrectedTranscript, &suggestionsBytes,
		&response.PracticeType, &response.AudioDataURL, &response.PhotoDataURL,
		&response.PhotoObject, &response.Status, &response.ProcessingStage,
		&response.ProcessingError, &response.ShadowingStatus,
		&response.ShadowingAudioURL, &response.ShadowingError, &shadowingUpdatedAt,
	)
	if err != nil {
		return recordingPageItem{}, err
	}
	response.Duration = domain.ToNonNegativeInt(response.Duration)
	response.Timestamp = timestamp.UTC().Format(time.RFC3339Nano)
	response.Status = normalizeRecordingStatus(response.Status)
	response.Suggestions = normalizeSuggestions(suggestionsBytes, 0)
	response.ProcessingStage = normalizeRecordingProcessingStage(response.ProcessingStage)
	response.PracticeType = domain.NormalizePracticeType(response.PracticeType)
	response.AudioDataURL = normalizeOptionalAudio(response.AudioDataURL, true)
	response.PhotoDataURL = normalizeOptionalPhoto(response.PhotoDataURL)
	response.PhotoObject = normalizeOptionalPhotoObject(response.PhotoObject)
	response.ProcessingError = normalizeOptionalProcessingError(response.ProcessingError)
	response.ShadowingStatus = normalizeShadowingStatus(response.ShadowingStatus)
	response.ShadowingAudioURL = normalizeOptionalShadowingAudio(response.ShadowingAudioURL)
	response.ShadowingError = normalizeOptionalProcessingError(response.ShadowingError)
	response.ShadowingUpdatedAt = shadowingUpdatedAt.UTC().Format(time.RFC3339Nano)
	return recordingPageItem{recording: response, timestamp: timestamp.UTC()}, nil
}
