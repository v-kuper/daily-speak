package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Server) handleDeleteRecording(w http.ResponseWriter, r *http.Request, recordingID string) {
	started := time.Now()
	logger := logging.ForRequest("api.recordings.delete", r)
	user, ok := s.authorizedUser(w, r, "api.recordings.delete")
	if !ok {
		return
	}
	recordingID = strings.TrimSpace(pathUnescape(recordingID))
	if recordingID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording ID is required."})
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var recordingAudioURL, shadowingAudioURL, recordingAudioAssetID, recordingPhotoAssetID, shadowingAssetID *string
	err = tx.QueryRow(r.Context(), `
		SELECT audio_data_url, shadowing_audio_url, audio_asset_id, photo_asset_id, shadowing_asset_id
		FROM recordings
		WHERE id = $1 AND user_id = $2
		FOR UPDATE`, recordingID, user.ID).Scan(
		&recordingAudioURL, &shadowingAudioURL, &recordingAudioAssetID,
		&recordingPhotoAssetID, &shadowingAssetID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}

	fileURLs := make([]string, 0, 4)
	fileURLSet := map[string]struct{}{}
	appendFileURL := func(value *string) {
		if value == nil {
			return
		}
		normalized := strings.TrimSpace(*value)
		if _, exists := fileURLSet[normalized]; exists {
			return
		}
		if _, pathErr := storedUploadPath(normalized); pathErr != nil {
			return
		}
		fileURLSet[normalized] = struct{}{}
		fileURLs = append(fileURLs, normalized)
	}
	appendFileURL(recordingAudioURL)
	appendFileURL(shadowingAudioURL)
	assetIDs := make([]string, 0, 6)
	assetIDSet := map[string]struct{}{}
	appendAssetID := func(value *string) {
		if value == nil {
			return
		}
		normalized := strings.TrimSpace(*value)
		if normalized == "" {
			return
		}
		if _, exists := assetIDSet[normalized]; exists {
			return
		}
		assetIDSet[normalized] = struct{}{}
		assetIDs = append(assetIDs, normalized)
	}
	appendAssetID(recordingAudioAssetID)
	appendAssetID(recordingPhotoAssetID)
	appendAssetID(shadowingAssetID)

	postIDs := []string{}
	postRows, err := tx.Query(r.Context(), `
		SELECT id, audio_data_url, audio_asset_id, photo_asset_id
		FROM feed_posts
		WHERE source_recording_id = $1
		FOR UPDATE`, recordingID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}
	for postRows.Next() {
		var postID string
		var audioURL, audioAssetID, photoAssetID *string
		if err := postRows.Scan(&postID, &audioURL, &audioAssetID, &photoAssetID); err != nil {
			postRows.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
			return
		}
		postIDs = append(postIDs, postID)
		appendFileURL(audioURL)
		appendAssetID(audioAssetID)
		appendAssetID(photoAssetID)
	}
	postRowsErr := postRows.Err()
	postRows.Close()
	if postRowsErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}

	if len(postIDs) > 0 {
		replyRows, queryErr := tx.Query(r.Context(), `
			SELECT audio_data_url, audio_asset_id
			FROM feed_replies
			WHERE post_id = ANY($1::text[])
			FOR UPDATE`, postIDs)
		if queryErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
			return
		}
		for replyRows.Next() {
			var audioURL, audioAssetID *string
			if err := replyRows.Scan(&audioURL, &audioAssetID); err != nil {
				replyRows.Close()
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
				return
			}
			appendFileURL(audioURL)
			appendAssetID(audioAssetID)
		}
		replyRowsErr := replyRows.Err()
		replyRows.Close()
		if replyRowsErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
			return
		}
	}

	for _, fileURL := range fileURLs {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO pending_file_deletions (public_url)
			VALUES ($1)
			ON CONFLICT (public_url) DO NOTHING`, fileURL); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
			return
		}
		jobID := uuid.NewString()
		if err := workqueue.Enqueue(r.Context(), tx, workqueue.NewJob{
			ID:             jobID,
			Kind:           workqueue.KindMediaDelete,
			ResourceID:     fileURL,
			IdempotencyKey: "media.delete:" + fileURL,
			MaxAttempts:    20,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
			return
		}
	}
	for _, assetID := range assetIDs {
		if _, err := tx.Exec(r.Context(), `
			UPDATE media_assets
			SET state = 'deleting', retention_until = NOW(), updated_at = NOW()
			WHERE id = $1 AND state <> 'deleted'`, assetID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
			return
		}
		jobID := uuid.NewString()
		if err := workqueue.Enqueue(r.Context(), tx, workqueue.NewJob{
			ID: jobID, Kind: workqueue.KindMediaDelete, ResourceID: assetID,
			IdempotencyKey: "media.delete:asset:" + assetID, MaxAttempts: 20,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE processing_jobs
		SET state = 'cancelled', completed_at = NOW(), updated_at = NOW(),
		    lease_token = NULL, lease_owner = NULL, lease_expires_at = NULL, heartbeat_at = NULL
		WHERE resource_id = $1
		  AND kind IN ('recording.process', 'shadowing.synthesize')
		  AND state IN ('queued', 'running', 'retry_wait')`, recordingID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}
	if _, err := tx.Exec(r.Context(), `
		DELETE FROM recording_upload_sessions
		WHERE recording_id = $1`, recordingID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}

	var deletedRecordingID string
	err = tx.QueryRow(r.Context(), `
		DELETE FROM recordings
		WHERE id = $1 AND user_id = $2
		RETURNING id`, recordingID, user.ID).Scan(&deletedRecordingID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete recording."})
		return
	}

	var quotaResponse *quota.RecordingQuota
	if currentQuota, quotaErr := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &user.IsSubscriber); quotaErr == nil {
		quotaResponse = &currentQuota
	} else {
		logger.Warn("recording.delete_quota_refresh_failed", logging.ErrorMeta(quotaErr))
	}
	logger.Info("request.success", map[string]any{
		"status":      http.StatusOK,
		"durationMs":  logging.ElapsedMs(started),
		"userId":      user.ID,
		"recordingId": deletedRecordingID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deletedRecordingId": deletedRecordingID, "quota": quotaResponse})
}
