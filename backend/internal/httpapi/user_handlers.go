package httpapi

import (
	"errors"
	"net/http"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/profile"
	"daily-speaking-practice/backend/internal/subscription"
)

func (s *Server) handleGetEnglishLevel(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	level, err := s.profileService.EnglishLevel(r.Context(), identity.User.ID)
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "profile_unavailable", "Failed to load English level")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"level": level})
}

func (s *Server) handlePutEnglishLevel(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	var payload struct {
		Level string `json:"level"`
	}
	if !decodeIdentityJSON(w, r, &payload) {
		return
	}
	level, err := s.profileService.SaveEnglishLevel(r.Context(), identity.User.ID, payload.Level)
	if err != nil {
		if errors.Is(err, profile.ErrInvalidEnglishLevel) {
			writeV1Error(w, r, http.StatusBadRequest, "invalid_english_level", "English level is invalid")
			return
		}
		writeV1Error(w, r, http.StatusInternalServerError, "profile_unavailable", "Failed to save English level")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"level": level})
}

func (s *Server) handleUserInterests(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	var payload struct {
		InterestIDs []string `json:"interestIds"`
	}
	if !decodeIdentityJSON(w, r, &payload) {
		return
	}
	interestIDs, err := s.profileService.ReplaceInterests(r.Context(), identity.User.ID, payload.InterestIDs)
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "profile_unavailable", "Failed to save interests")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interestIds": interestIDs})
}

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	overview, err := s.subscriptionService.Get(r.Context(), identity.User.ID)
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "subscription_unavailable", "Failed to load subscription")
		return
	}
	writeSubscriptionOverview(w, overview)
}

func (s *Server) handleActivateSubscription(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	overview, err := s.subscriptionService.Activate(r.Context(), identity.User.ID)
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "subscription_unavailable", "Failed to activate subscription")
		return
	}
	writeSubscriptionOverview(w, overview)
}

func (s *Server) handleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	overview, err := s.subscriptionService.Cancel(r.Context(), identity.User.ID)
	if err != nil {
		if errors.Is(err, subscription.ErrNoActiveSubscription) {
			writeV1Error(w, r, http.StatusConflict, "subscription_not_active", "No active subscription to cancel")
			return
		}
		writeV1Error(w, r, http.StatusInternalServerError, "subscription_unavailable", "Failed to cancel subscription")
		return
	}
	writeSubscriptionOverview(w, overview)
}

func (s *Server) handleUserData(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.v1.profile.get", r)
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}

	interestIDs, err := s.profileService.Interests(r.Context(), identity.User.ID)
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "profile_unavailable", "Failed to load profile")
		return
	}
	overview, err := s.subscriptionService.Get(r.Context(), identity.User.ID)
	if err != nil {
		writeV1Error(w, r, http.StatusInternalServerError, "profile_unavailable", "Failed to load profile")
		return
	}

	logger.Info("request.success", map[string]any{
		"status":         200,
		"durationMs":     logging.ElapsedMs(started),
		"userId":         identity.User.ID,
		"interestsCount": len(interestIDs),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"interestIds":  interestIDs,
		"quota":        overview.Quota,
		"subscription": subscriptionResponseFrom(overview.State),
		"englishLevel": identity.User.EnglishLevel,
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
