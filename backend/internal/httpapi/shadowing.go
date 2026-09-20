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
	var correctedTranscript string
	err := s.db.QueryRow(ctx, `
		UPDATE recordings
		SET shadowing_status = 'processing',
		    shadowing_error = NULL,
		    shadowing_updated_at = NOW()
		WHERE id = $1
		  AND user_id = $2
		  AND BTRIM(corrected_transcript) <> ''
		  AND (
		    shadowing_status IN ('pending', 'failed')
		    OR (shadowing_status = 'processing' AND shadowing_updated_at < NOW() - INTERVAL '5 minutes')
		  )
		RETURNING corrected_transcript`, recordingID, userID).Scan(&correctedTranscript)
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

	recording, err := s.recordingForUser(ctx, userID, recordingID)
	if err != nil {
		_, _ = s.db.Exec(context.Background(), `
			UPDATE recordings
			SET shadowing_status = 'failed', shadowing_error = $3, shadowing_updated_at = NOW()
			WHERE id = $1 AND user_id = $2`, recordingID, userID, shadowingFailureMessage)
		return recordingResponse{}, false, err
	}
	s.startShadowingJob(recordingID, userID, correctedTranscript)
	return recording, true, nil
}

func (s *Server) startShadowingJob(recordingID string, userID string, correctedTranscript string) {
	ctx, cancel := context.WithTimeout(context.Background(), shadowingJobTimeout)
	jobID := uuid.NewString()
	s.registerShadowingProcessing(recordingID, shadowingJob{id: jobID, cancel: cancel})
	go func() {
		defer cancel()
		defer s.unregisterShadowingProcessing(recordingID, jobID)
		s.runShadowing(ctx, recordingID, userID, correctedTranscript)
	}()
}

func (s *Server) runShadowing(ctx context.Context, recordingID string, userID string, correctedTranscript string) {
	started := time.Now()
	logger := logging.ForBackground("api.recordings.shadowing")
	audio, err := s.synthesizer.Synthesize(ctx, correctedTranscript)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			logger.Info("shadowing.cancelled", map[string]any{"recordingId": recordingID})
			return
		}
		s.failShadowing(recordingID, userID, logger)
		return
	}
	saved, err := saveShadowingAudio(userID, recordingID, audio)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		s.failShadowing(recordingID, userID, logger)
		return
	}

	result, err := s.db.Exec(ctx, `
		UPDATE recordings
		SET shadowing_status = 'ready',
		    shadowing_audio_url = $2,
		    shadowing_error = NULL,
		    shadowing_updated_at = NOW()
		WHERE id = $1 AND user_id = $3 AND shadowing_status = 'processing'`, recordingID, saved.publicURL, userID)
	if err != nil || result.RowsAffected() == 0 {
		_ = os.Remove(saved.absolutePath)
		if !errors.Is(ctx.Err(), context.Canceled) {
			s.failShadowing(recordingID, userID, logger)
		}
		return
	}
	logger.Info("shadowing.ready", map[string]any{
		"recordingId": recordingID,
		"durationMs":  logging.ElapsedMs(started),
		"bytes":       len(audio),
	})
}

func (s *Server) failShadowing(recordingID string, userID string, logger logging.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.Exec(ctx, `
		UPDATE recordings
		SET shadowing_status = 'failed',
		    shadowing_audio_url = NULL,
		    shadowing_error = $3,
		    shadowing_updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND shadowing_status = 'processing'`, recordingID, userID, shadowingFailureMessage)
	meta := map[string]any{"recordingId": recordingID}
	if err != nil {
		meta["statusUpdate"] = "failed"
	}
	logger.Error("shadowing.failed", meta)
}

func (s *Server) registerShadowingProcessing(recordingID string, job shadowingJob) {
	s.shadowingProcessingMu.Lock()
	previous := s.shadowingProcessingJobs[recordingID]
	s.shadowingProcessingJobs[recordingID] = job
	s.shadowingProcessingMu.Unlock()
	if previous.cancel != nil {
		previous.cancel()
	}
}

func (s *Server) unregisterShadowingProcessing(recordingID string, jobID string) {
	s.shadowingProcessingMu.Lock()
	if current := s.shadowingProcessingJobs[recordingID]; current.id == jobID {
		delete(s.shadowingProcessingJobs, recordingID)
	}
	s.shadowingProcessingMu.Unlock()
}

func (s *Server) cancelShadowingProcessing(recordingID string) {
	s.shadowingProcessingMu.Lock()
	job := s.shadowingProcessingJobs[recordingID]
	delete(s.shadowingProcessingJobs, recordingID)
	s.shadowingProcessingMu.Unlock()
	if job.cancel != nil {
		job.cancel()
	}
}

func saveShadowingAudio(userID string, recordingID string, audio []byte) (savedAudioFile, error) {
	if len(audio) == 0 || len(audio) > maxShadowingAudioBytes {
		return savedAudioFile{}, errors.New("shadowing audio payload is invalid")
	}
	userSegment := domain.SanitizePathSegment(userID)
	recordingSegment := domain.SanitizePathSegment(recordingID)
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

	fileName := recordingSegment + ".mp3"
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
