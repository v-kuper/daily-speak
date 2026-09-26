package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	shadowingJobTimeout     = 2 * time.Minute
	maxShadowingAudioBytes  = 25 * 1024 * 1024
	shadowingFailureMessage = "Pronunciation audio could not be generated. Check the Cartesia configuration or try again."
)

var errShadowingTranscriptUnavailable = errors.New("corrected transcript is unavailable")

func (s *Server) routeShadowingPath(w http.ResponseWriter, r *http.Request, relativePath string) {
	parts := strings.Split(strings.Trim(relativePath, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "shadowing" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	s.handleGenerateShadowing(w, r, pathUnescape(parts[0]))
}

func (s *Server) handleGenerateShadowing(w http.ResponseWriter, r *http.Request, recordingID string) {
	started := time.Now()
	logger := logging.ForRequest("api.recordings.shadowing", r)
	user, ok := s.authorizedUser(w, r, "api.recordings.shadowing")
	if !ok {
		return
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording ID is required."})
		return
	}

	recording, scheduled, err := s.scheduleShadowing(r.Context(), user.ID, recordingID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if errors.Is(err, errShadowingTranscriptUnavailable) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "The natural transcript is not ready yet."})
		return
	}
	if err != nil {
		logger.Error("shadowing.schedule_failed", map[string]any{"recordingId": recordingID})
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to generate pronunciation audio."})
		return
	}
	logger.Info("request.success", map[string]any{
		"status":      http.StatusOK,
		"durationMs":  logging.ElapsedMs(started),
		"recordingId": recordingID,
		"scheduled":   scheduled,
	})
	writeJSON(w, http.StatusOK, map[string]any{"recording": recording})
}

func (s *Server) scheduleShadowing(ctx context.Context, userID string, recordingID string) (recordingResponse, bool, error) {
	userID = strings.TrimSpace(userID)
	recordingID = strings.TrimSpace(recordingID)
	attemptID := uuid.NewString()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return recordingResponse{}, false, err
	}
	defer tx.Rollback(ctx)
	var correctedTranscript string
	err = tx.QueryRow(ctx, `
		UPDATE recordings
		SET shadowing_status = 'processing',
		    shadowing_error = NULL,
		    shadowing_updated_at = NOW(),
		    shadowing_attempt_id = $3
		WHERE id = $1
		  AND user_id = $2
		  AND BTRIM(corrected_transcript) <> ''
		  AND (
		    shadowing_status IN ('pending', 'failed')
		    OR (shadowing_status = 'processing' AND shadowing_attempt_id IS NULL AND shadowing_updated_at < NOW() - INTERVAL '5 minutes')
		  )
		RETURNING corrected_transcript`, recordingID, userID, attemptID).Scan(&correctedTranscript)
	if errors.Is(err, pgx.ErrNoRows) {
		recording, loadErr := s.recordingForUser(ctx, userID, recordingID)
		if loadErr != nil {
			return recordingResponse{}, false, loadErr
		}
		if strings.TrimSpace(recording.CorrectedTranscript) == "" {
			return recordingResponse{}, false, errShadowingTranscriptUnavailable
		}
		return recording, false, nil
	}
	if err != nil {
		return recordingResponse{}, false, err
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
		ID:             attemptID,
		Kind:           workqueue.KindShadowingSynthesize,
		ResourceID:     recordingID,
		IdempotencyKey: "shadowing:" + attemptID,
		MaxAttempts:    4,
	}); err != nil {
		return recordingResponse{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordingResponse{}, false, err
	}

	recording, err := s.recordingForUser(ctx, userID, recordingID)
	if err != nil {
		return recordingResponse{}, false, err
	}
	return recording, true, nil
}

func (s *Server) runShadowingJob(ctx context.Context, job workqueue.Job) error {
	var userID, correctedTranscript, status string
	var attemptID *string
	err := s.db.QueryRow(ctx, `
		SELECT user_id, corrected_transcript, shadowing_status, shadowing_attempt_id
		FROM recordings
		WHERE id = $1`, job.ResourceID).Scan(&userID, &correctedTranscript, &status, &attemptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "processing" || attemptID == nil || *attemptID != job.ID {
		return nil
	}
	return s.runShadowing(ctx, job.ResourceID, userID, correctedTranscript, job.ID, job.LeaseToken)
}

func (s *Server) runShadowing(ctx context.Context, recordingID string, userID string, correctedTranscript string, attemptID string, leaseToken string) error {
	started := time.Now()
	logger := logging.ForBackground("worker.recordings.shadowing")
	audio, err := s.synthesizer.Synthesize(ctx, correctedTranscript)
	if err != nil {
		return err
	}
	// Include the fenced lease token in the object name. A worker that finishes
	// after losing its lease can therefore never overwrite the active worker's
	// published audio before its conditional database update is rejected.
	saved, err := saveShadowingAudio(userID, recordingID, attemptID+"-"+leaseToken, audio)
	if err != nil {
		return err
	}

	result, err := s.db.Exec(ctx, `
		UPDATE recordings
		SET shadowing_status = 'ready',
		    shadowing_audio_url = $2,
		    shadowing_error = NULL,
		    shadowing_updated_at = NOW(),
		    shadowing_attempt_id = NULL
		WHERE id = $1 AND user_id = $3 AND shadowing_status = 'processing' AND shadowing_attempt_id = $4
		  AND EXISTS (
		    SELECT 1 FROM processing_jobs
		    WHERE id = $4 AND state = 'running' AND lease_token = $5
		  )`, recordingID, saved.publicURL, userID, attemptID, leaseToken)
	if err != nil || result.RowsAffected() == 0 {
		_ = os.Remove(saved.absolutePath)
		if err != nil {
			return err
		}
		return nil
	}
	logger.Info("shadowing.ready", map[string]any{
		"recordingId": recordingID,
		"durationMs":  logging.ElapsedMs(started),
		"bytes":       len(audio),
	})
	return nil
}

func saveShadowingAudio(userID string, recordingID string, attemptID string, audio []byte) (savedAudioFile, error) {
	if len(audio) == 0 || len(audio) > maxShadowingAudioBytes {
		return savedAudioFile{}, errors.New("shadowing audio payload is invalid")
	}
	userSegment := domain.SanitizePathSegment(userID)
	recordingSegment := domain.SanitizePathSegment(recordingID)
	attemptSegment := domain.SanitizePathSegment(attemptID)
	directory := filepath.Join(resolveUploadsDir(), "shadowing", userSegment)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return savedAudioFile{}, err
	}
	temporary, err := os.CreateTemp(directory, ".shadowing-*.tmp")
	if err != nil {
		return savedAudioFile{}, err
	}
	temporaryPath := temporary.Name()
	cleanupTemporary := true
	defer func() {
		_ = temporary.Close()
		if cleanupTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(audio); err != nil {
		return savedAudioFile{}, err
	}
	if err := temporary.Sync(); err != nil {
		return savedAudioFile{}, err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return savedAudioFile{}, err
	}
	if err := temporary.Close(); err != nil {
		return savedAudioFile{}, err
	}

	fileName := recordingSegment + "-" + attemptSegment + ".mp3"
	absolutePath := filepath.Join(directory, fileName)
	if err := os.Remove(absolutePath); err != nil && !os.IsNotExist(err) {
		return savedAudioFile{}, err
	}
	if err := os.Rename(temporaryPath, absolutePath); err != nil {
		return savedAudioFile{}, err
	}
	cleanupTemporary = false
	return savedAudioFile{
		publicURL:    "/uploads/shadowing/" + userSegment + "/" + fileName,
		absolutePath: absolutePath,
	}, nil
}
