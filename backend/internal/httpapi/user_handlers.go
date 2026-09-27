package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/profile"
	"daily-speaking-practice/backend/internal/subscription"
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
	level, err := s.profileService.EnglishLevel(r.Context(), user.ID)
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
	level, err := s.profileService.SaveEnglishLevel(r.Context(), user.ID, payload.Level)
	if err != nil {
		if errors.Is(err, profile.ErrInvalidEnglishLevel) {
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
	interestIDs, err := s.profileService.ReplaceInterests(r.Context(), user.ID, payload.InterestIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save interests."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interestIds": interestIDs})
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.subscription.get")
	if !ok {
		return
	}
	overview, err := s.subscriptionService.Get(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load subscription."})
		return
	}
	writeSubscriptionOverview(w, overview)
}

func (s *Server) handleActivateSubscription(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.subscription.post")
	if !ok {
		return
	}
	overview, err := s.subscriptionService.Activate(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to activate subscription."})
		return
	}
	writeSubscriptionOverview(w, overview)
}

func (s *Server) handleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.user.subscription.delete")
	if !ok {
		return
	}
	overview, err := s.subscriptionService.Cancel(r.Context(), user.ID)
	if err != nil {
		if errors.Is(err, subscription.ErrNoActiveSubscription) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No active subscription to cancel."})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to cancel subscription."})
		return
	}
	writeSubscriptionOverview(w, overview)
}

func (s *Server) handleUserData(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.user.data.get", r)
	user, ok := s.authorizedUser(w, r, "api.user.data.get")
	if !ok {
		return
	}

	interestIDs, err := s.profileService.Interests(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user data."})
		return
	}
	overview, err := s.subscriptionService.Get(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load user data."})
		return
	}

	logger.Info("request.success", map[string]any{
		"status":         200,
		"durationMs":     logging.ElapsedMs(started),
		"userId":         user.ID,
		"interestsCount": len(interestIDs),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"interestIds":  interestIDs,
		"quota":        overview.Quota,
		"subscription": subscriptionResponseFrom(overview.State),
		"englishLevel": user.EnglishLevel,
	})
}

type subscriptionResponse struct {
	IsSubscriber          bool    `json:"isSubscriber"`
	SubscriptionExpiresAt *string `json:"subscriptionExpiresAt"`
	SubscriptionCancelled bool    `json:"subscriptionCancelled"`
}

func subscriptionResponseFrom(state subscription.State) subscriptionResponse {
	var expiresAt *string
	if state.ExpiresAt != nil {
		value := state.ExpiresAt.UTC().Format(time.RFC3339Nano)
		expiresAt = &value
	}
	return subscriptionResponse{
		IsSubscriber: state.IsSubscriber, SubscriptionExpiresAt: expiresAt,
		SubscriptionCancelled: state.Cancelled,
	}
}

func writeSubscriptionOverview(w http.ResponseWriter, overview subscription.Overview) {
	writeJSON(w, http.StatusOK, map[string]any{
		"subscription": subscriptionResponseFrom(overview.State),
		"quota":        overview.Quota,
	})
}

func normalizeOptionalPhotoObject(value *string) *string {
	if value == nil {
		return nil
	}
	return media.NormalizePhotoObject(*value)
}
