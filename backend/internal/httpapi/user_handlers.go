package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/profile"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/subscription"
	"github.com/jackc/pgx/v5"
)

func (s *Server) handleUserOllamaModel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedUser(w, r, "api.user.ollama-model.get"); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"selectedModel":   ai.DefaultModel(),
		"isThinkingModel": ai.DefaultIsThinkingModel(),
		"warning":         nil,
	})
}

func (s *Server) handleGetEnglishLevel(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.english-level.get")
	if !ok {
		return
	}
	level, err := profile.GetEnglishLevel(r.Context(), s.db, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load English level."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"level": level})
}

func (s *Server) handlePutEnglishLevel(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.english-level.put")
	if !ok {
		return
	}
	var payload struct {
		Level string `json:"level"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	level, err := profile.SaveEnglishLevel(r.Context(), s.db, user.ID, payload.Level)
	if err != nil {
		if err.Error() == "English level is invalid." {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "English level is invalid."})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save English level."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"level": level})
}

func (s *Server) handleUserInterests(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.interests.put")
	if !ok {
		return
	}
	var payload struct {
		InterestIDs []string `json:"interestIds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	interestIDs := domain.NormalizeInterests(payload.InterestIDs, 10)
	if _, err := s.db.Exec(r.Context(), `DELETE FROM user_interests WHERE user_id = $1`, user.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save interests."})
		return
	}
	if len(interestIDs) > 0 {
		_, err := s.db.Exec(r.Context(), `
			INSERT INTO user_interests (user_id, interest_id)
			SELECT $1, interest_id
			FROM UNNEST($2::text[]) AS t(interest_id)
			ON CONFLICT DO NOTHING`, user.ID, interestIDs)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save interests."})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"interestIds": interestIDs})
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.subscription.get")
	if !ok {
		return
	}
	state, err := subscription.GetState(r.Context(), s.db, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load subscription."})
		return
	}
	q, err := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &state.IsSubscriber)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load subscription."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscription": state, "quota": q})
}

func (s *Server) handleActivateSubscription(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.subscription.post")
	if !ok {
		return
	}
	_, err := s.db.Exec(r.Context(), `
		UPDATE users
		SET
		  is_subscriber = TRUE,
		  subscription_cancelled = FALSE,
		  subscription_expires_at = CASE
		    WHEN subscription_expires_at IS NOT NULL AND subscription_expires_at > NOW()
		      THEN subscription_expires_at + INTERVAL '1 month'
		    ELSE NOW() + INTERVAL '1 month'
		  END
		WHERE id = $1`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to activate subscription."})
		return
	}
	s.handleGetSubscription(w, r)
}

func (s *Server) handleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.subscription.delete")
	if !ok {
		return
	}
	tag, err := s.db.Exec(r.Context(), `
		UPDATE users
		SET
		  subscription_cancelled = TRUE,
		  subscription_expires_at = COALESCE(subscription_expires_at, NOW() + INTERVAL '1 month')
		WHERE id = $1
		  AND is_subscriber = TRUE
		  AND (subscription_expires_at IS NULL OR subscription_expires_at > NOW())`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to cancel subscription."})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No active subscription to cancel."})
		return
	}
	s.handleGetSubscription(w, r)
}

func (s *Server) handleUserData(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.user.data.get", r)
	user, ok := s.authorizedUser(w, r, "api.user.data.get")
	if !ok {
		return
	}

	interestRows, err := s.db.Query(r.Context(), `
		SELECT interest_id
		FROM user_interests
		WHERE user_id = $1
		ORDER BY created_at ASC`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user data."})
		return
	}
	defer interestRows.Close()
	interestIDs := []string{}
	for interestRows.Next() {
		var interestID string
		if err := interestRows.Scan(&interestID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user data."})
			return
		}
		interestIDs = append(interestIDs, interestID)
	}

	recordingRows, err := s.db.Query(r.Context(), `
		SELECT
		  id, topic, duration, timestamp, transcript, suggestions,
		  practice_type, audio_data_url, photo_data_url, photo_object
		FROM recordings
		WHERE user_id = $1
		ORDER BY timestamp DESC`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user data."})
		return
	}
	defer recordingRows.Close()
	recordings := []recordingResponse{}
	for recordingRows.Next() {
		var id, topic, transcript, practiceType string
		var duration int
		var timestamp time.Time
		var suggestionsBytes []byte
		var audioDataURL, photoDataURL, photoObject *string
		if err := recordingRows.Scan(&id, &topic, &duration, &timestamp, &transcript, &suggestionsBytes, &practiceType, &audioDataURL, &photoDataURL, &photoObject); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user data."})
			return
		}
		recordings = append(recordings, recordingResponse{
			ID:           id,
			Topic:        topic,
			Duration:     domain.ToNonNegativeInt(duration),
			Timestamp:    timestamp.UTC().Format(time.RFC3339Nano),
			Transcript:   transcript,
			Suggestions:  normalizeSuggestions(suggestionsBytes, 20),
			PracticeType: domain.NormalizePracticeType(practiceType),
			AudioDataURL: normalizeOptionalAudio(audioDataURL, true),
			PhotoDataURL: normalizeOptionalPhoto(photoDataURL),
			PhotoObject:  normalizeOptionalPhotoObject(photoObject),
		})
	}

	state, err := subscription.GetState(r.Context(), s.db, user.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user data."})
		return
	}
	q, err := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &state.IsSubscriber)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user data."})
		return
	}

	logger.Info("request.success", map[string]any{
		"status":          200,
		"durationMs":      logging.ElapsedMs(started),
		"userId":          user.ID,
		"interestsCount":  len(interestIDs),
		"recordingsCount": len(recordings),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"interestIds":  interestIDs,
		"recordings":   recordings,
		"quota":        q,
		"subscription": state,
		"englishLevel": user.EnglishLevel,
	})
}

func normalizeOptionalAudio(value *string, recordingOnly bool) *string {
	if value == nil {
		return nil
	}
	if recordingOnly {
		return domain.NormalizeStoredRecordingAudioSource(*value)
	}
	return domain.NormalizeStoredGenericAudioSource(*value)
}

func normalizeOptionalPhoto(value *string) *string {
	if value == nil {
		return nil
	}
	return domain.NormalizePhotoDataURL(*value)
}

func normalizeOptionalPhotoObject(value *string) *string {
	if value == nil {
		return nil
	}
	return domain.NormalizePhotoObject(*value)
}
