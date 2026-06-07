package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/quota"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type recordingSessionRow struct {
	ID             string
	UserID         string
	Topic          string
	Duration       int
	Timestamp      time.Time
	PracticeType   string
	PhotoDataURL   *string
	PhotoObject    *string
	AudioExtension *string
	ChunkCount     int
	Status         string
	RecordingID    *string
}

func (s *Server) handleCreateRecordingSession(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.recording-sessions.post")
	if !ok {
		return
	}

	var payload struct {
		Topic        string `json:"topic"`
		Duration     int    `json:"duration"`
		Timestamp    string `json:"timestamp"`
		PracticeType string `json:"practiceType"`
		PhotoDataURL string `json:"photoDataUrl"`
		PhotoObject  string `json:"photoObject"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	practiceType := domain.NormalizePracticeType(payload.PracticeType)
	topic := strings.TrimSpace(payload.Topic)
	duration := domain.ToNonNegativeInt(payload.Duration)
	photoDataURL := domain.NormalizePhotoDataURL(payload.PhotoDataURL)
	photoObject := domain.NormalizePhotoObject(payload.PhotoObject)
	if practiceType == "photo_description" {
		if photoDataURL == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Photo is required for photo description practice."})
			return
		}
		if topic == "" {
			topic = "Photo description"
		}
	}
	if topic == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording topic is required."})
		return
	}

	sessionID := uuid.NewString()
	timestamp := domain.ParseTimestamp(payload.Timestamp)
	_, err := s.db.Exec(r.Context(), `
		INSERT INTO recording_upload_sessions
		  (id, user_id, topic, duration, timestamp, practice_type, photo_data_url, photo_object)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sessionID,
		user.ID,
		truncateRunes(topic, 300),
		duration,
		timestamp,
		practiceType,
		stringOrNil(practiceType == "photo_description", photoDataURL),
		stringOrNil(practiceType == "photo_description", photoObject),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to start recording upload."})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"sessionId": sessionID, "chunkSeconds": 5})
}

func (s *Server) routeRecordingSessionPath(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	sessionID := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])
	switch {
	case action == "chunks" && r.Method == http.MethodPost:
		s.handleUploadRecordingSessionChunk(w, r, sessionID)
	case action == "finish" && r.Method == http.MethodPost:
		s.handleFinishRecordingSession(w, r, sessionID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	}
}

func (s *Server) handleUploadRecordingSessionChunk(w http.ResponseWriter, r *http.Request, sessionID string) {
	user, ok := s.authorizedUser(w, r, "api.recording-sessions.chunks.post")
	if !ok {
		return
	}
	session, err := s.recordingSessionForUser(r.Context(), user.ID, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording upload session not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load recording upload session."})
		return
	}
	if session.Status != "open" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording upload session is already finalized."})
		return
	}
	chunk, err := readMultipartChunkRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording chunk is invalid."})
		return
	}
	if session.AudioExtension != nil && *session.AudioExtension != chunk.Extension {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording chunk format changed during upload."})
		return
	}
	if _, err := saveRecordingSessionChunk(session.ID, chunk.Index, chunk.Extension, chunk.Bytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording chunk."})
		return
	}
	_, err = s.db.Exec(r.Context(), `
		UPDATE recording_upload_sessions
		SET audio_extension = COALESCE(audio_extension, $3),
		    chunk_count = GREATEST(chunk_count, $4),
		    updated_at = NOW()
		WHERE id = $1 AND user_id = $2`,
		session.ID, user.ID, chunk.Extension, chunk.Index+1)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording chunk."})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"sessionId": session.ID, "chunkIndex": chunk.Index})
}

func (s *Server) handleFinishRecordingSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	user, ok := s.authorizedUser(w, r, "api.recording-sessions.finish.post")
	if !ok {
		return
	}
	var payload struct {
		Duration  int    `json:"duration"`
		Timestamp string `json:"timestamp"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	session, err := s.recordingSessionForUser(r.Context(), user.ID, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording upload session not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to finish recording upload."})
		return
	}
	if session.Status != "open" {
		if session.RecordingID != nil {
			recording, err := s.recordingForUser(r.Context(), user.ID, *session.RecordingID)
			if err == nil {
				writeJSON(w, http.StatusOK, map[string]any{"recording": recording})
				return
			}
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording upload session is already finalized."})
		return
	}
	if session.AudioExtension == nil || session.ChunkCount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Upload at least one recording chunk before saving."})
		return
	}

	duration := session.Duration
	if payload.Duration > 0 {
		duration = domain.ToNonNegativeInt(payload.Duration)
	}
	timestamp := session.Timestamp
	if strings.TrimSpace(payload.Timestamp) != "" {
		timestamp = domain.ParseTimestamp(payload.Timestamp)
	}

	qBefore, err := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &user.IsSubscriber)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	if quotaError := recordingQuotaError(qBefore, duration); quotaError != nil {
		writeJSON(w, quotaError.status, map[string]string{"error": quotaError.message})
		return
	}

	recordingID := uuid.NewString()
	audioPath := filepath.Join(resolveUploadsDir(), "recordings", domain.SanitizePathSegment(user.ID), recordingID+"."+*session.AudioExtension)
	if err := assembleRecordingSessionChunks(session.ID, *session.AudioExtension, session.ChunkCount, audioPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to assemble recording audio."})
		return
	}
	audioURL := "/uploads/recordings/" + domain.SanitizePathSegment(user.ID) + "/" + recordingID + "." + *session.AudioExtension
	var inserted struct {
		ID              string
		Topic           string
		Duration        int
		Timestamp       time.Time
		Status          string
		Transcript      string
		Suggestions     []byte
		PracticeType    string
		AudioDataURL    *string
		PhotoDataURL    *string
		PhotoObject     *string
		ProcessingError *string
	}
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, suggestions, practice_type, audio_data_url, photo_data_url, photo_object, status)
		VALUES
		  ($1, $2, $3, $4, $5, '', '[]'::jsonb, $6, $7, $8, $9, 'processing')
		RETURNING id, topic, duration, timestamp, status, transcript, suggestions, practice_type, audio_data_url, photo_data_url, photo_object, processing_error`,
		recordingID,
		user.ID,
		session.Topic,
		duration,
		timestamp,
		session.PracticeType,
		audioURL,
		stringOrNil(session.PracticeType == "photo_description", session.PhotoDataURL),
		stringOrNil(session.PracticeType == "photo_description", session.PhotoObject),
	).Scan(&inserted.ID, &inserted.Topic, &inserted.Duration, &inserted.Timestamp, &inserted.Status, &inserted.Transcript, &inserted.Suggestions, &inserted.PracticeType, &inserted.AudioDataURL, &inserted.PhotoDataURL, &inserted.PhotoObject, &inserted.ProcessingError)
	if err != nil {
		_ = os.Remove(audioPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	recording := recordingResponse{
		ID:              inserted.ID,
		Topic:           inserted.Topic,
		Duration:        domain.ToNonNegativeInt(inserted.Duration),
		Timestamp:       inserted.Timestamp.UTC().Format(time.RFC3339Nano),
		Status:          normalizeRecordingStatus(inserted.Status),
		Transcript:      inserted.Transcript,
		Suggestions:     normalizeSuggestions(inserted.Suggestions, 20),
		PracticeType:    domain.NormalizePracticeType(inserted.PracticeType),
		AudioDataURL:    normalizeOptionalAudio(inserted.AudioDataURL, true),
		PhotoDataURL:    normalizeOptionalPhoto(inserted.PhotoDataURL),
		PhotoObject:     normalizeOptionalPhotoObject(inserted.PhotoObject),
		ProcessingError: normalizeOptionalProcessingError(inserted.ProcessingError),
	}

	_, err = s.db.Exec(r.Context(), `
		UPDATE recording_upload_sessions
		SET status = 'finalized', recording_id = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2`, session.ID, user.ID, recordingID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to finalize recording upload."})
		return
	}
	_ = os.RemoveAll(recordingSessionChunksDir(session.ID))
	q, err := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &user.IsSubscriber)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	s.processRecordingInBackground(recordingID, user.ID, audioPath, session.Topic, session.PracticeType, session.PhotoObject)
	writeJSON(w, http.StatusCreated, map[string]any{"recording": recording, "quota": q})
}

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

func (s *Server) recordingSessionForUser(ctx context.Context, userID string, sessionID string) (recordingSessionRow, error) {
	var row recordingSessionRow
	err := s.db.QueryRow(ctx, `
		SELECT id, user_id, topic, duration, timestamp, practice_type,
		       photo_data_url, photo_object, audio_extension, chunk_count,
		       status, recording_id
		FROM recording_upload_sessions
		WHERE id = $1 AND user_id = $2
		LIMIT 1`, strings.TrimSpace(sessionID), userID).Scan(&row.ID, &row.UserID, &row.Topic, &row.Duration, &row.Timestamp, &row.PracticeType, &row.PhotoDataURL, &row.PhotoObject, &row.AudioExtension, &row.ChunkCount, &row.Status, &row.RecordingID)
	return row, err
}

func (s *Server) recordingForUser(ctx context.Context, userID string, recordingID string) (recordingResponse, error) {
	var row struct {
		ID              string
		Topic           string
		Duration        int
		Timestamp       time.Time
		Status          string
		Transcript      string
		Suggestions     []byte
		PracticeType    string
		AudioDataURL    *string
		PhotoDataURL    *string
		PhotoObject     *string
		ProcessingError *string
	}
	err := s.db.QueryRow(ctx, `
		SELECT id, topic, duration, timestamp, status, transcript, suggestions,
		       practice_type, audio_data_url, photo_data_url, photo_object, processing_error
		FROM recordings
		WHERE id = $1 AND user_id = $2
		LIMIT 1`, strings.TrimSpace(recordingID), userID).Scan(&row.ID, &row.Topic, &row.Duration, &row.Timestamp, &row.Status, &row.Transcript, &row.Suggestions, &row.PracticeType, &row.AudioDataURL, &row.PhotoDataURL, &row.PhotoObject, &row.ProcessingError)
	if err != nil {
		return recordingResponse{}, err
	}
	return recordingResponse{
		ID:              row.ID,
		Topic:           row.Topic,
		Duration:        domain.ToNonNegativeInt(row.Duration),
		Timestamp:       row.Timestamp.UTC().Format(time.RFC3339Nano),
		Status:          normalizeRecordingStatus(row.Status),
		Transcript:      row.Transcript,
		Suggestions:     normalizeSuggestions(row.Suggestions, 20),
		PracticeType:    domain.NormalizePracticeType(row.PracticeType),
		AudioDataURL:    normalizeOptionalAudio(row.AudioDataURL, true),
		PhotoDataURL:    normalizeOptionalPhoto(row.PhotoDataURL),
		PhotoObject:     normalizeOptionalPhotoObject(row.PhotoObject),
		ProcessingError: normalizeOptionalProcessingError(row.ProcessingError),
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
