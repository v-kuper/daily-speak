package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/logging"
)

const maxIdentityRequestBytes = 16 << 10

type identityDevicePayload struct {
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
}

type identityCredentialsPayload struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
}

type identityResponse struct {
	Principal identityPrincipalResponse `json:"principal"`
	User      *auth.User                `json:"user,omitempty"`
	Session   identitySessionResponse   `json:"session"`
	Tokens    *identityTokensResponse   `json:"tokens,omitempty"`
}

type identityPrincipalResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type identitySessionResponse struct {
	ID         string `json:"id"`
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
	Current    bool   `json:"current"`
	ExpiresAt  string `json:"expiresAt"`
	LastSeenAt string `json:"lastSeenAt"`
	CreatedAt  string `json:"createdAt"`
}

type identityTokensResponse struct {
	TokenType             string `json:"tokenType"`
	AccessToken           string `json:"accessToken"`
	AccessTokenExpiresAt  string `json:"accessTokenExpiresAt"`
	RefreshToken          string `json:"refreshToken"`
	RefreshTokenExpiresAt string `json:"refreshTokenExpiresAt"`
}

func (s *Server) handleAnonymousIdentityV1(w http.ResponseWriter, r *http.Request) {
	var payload identityDevicePayload
	if r.Body != nil && r.ContentLength != 0 && !decodeIdentityJSON(w, r, &payload) {
		return
	}
	grant, err := auth.CreateAnonymousIdentity(r.Context(), s.db, s.identityTokens, auth.DeviceInfo{Name: payload.DeviceName, Platform: payload.Platform})
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, identityGrantResponse(grant))
}

func (s *Server) handleRegisterIdentityV1(w http.ResponseWriter, r *http.Request) {
	var payload identityCredentialsPayload
	if !decodeIdentityJSON(w, r, &payload) {
		return
	}
	credentials, err := auth.ValidateCredentials(payload.Email, payload.Password)
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	guest, ok := s.optionalGuestIdentityV1(w, r)
	if !ok {
		return
	}
	grant, err := auth.RegisterMobileUser(r.Context(), s.db, s.identityTokens, credentials, guest, auth.DeviceInfo{Name: payload.DeviceName, Platform: payload.Platform})
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, identityGrantResponse(grant))
}

func (s *Server) handleLoginIdentityV1(w http.ResponseWriter, r *http.Request) {
	var payload identityCredentialsPayload
	if !decodeIdentityJSON(w, r, &payload) {
		return
	}
	credentials, err := auth.ValidateCredentials(payload.Email, payload.Password)
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	guest, ok := s.optionalGuestIdentityV1(w, r)
	if !ok {
		return
	}
	grant, err := auth.LoginMobileUser(r.Context(), s.db, s.identityTokens, credentials, guest, auth.DeviceInfo{Name: payload.DeviceName, Platform: payload.Platform})
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, identityGrantResponse(grant))
}

func (s *Server) handleRefreshIdentityV1(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !decodeIdentityJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.RefreshToken) == "" {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "refreshToken is required")
		return
	}
	grant, err := auth.RotateRefreshToken(r.Context(), s.db, s.identityTokens, payload.RefreshToken)
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, identityGrantResponse(grant))
}

func (s *Server) handleIdentitySessionV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return
	}
	sessions, err := auth.ListDeviceSessions(r.Context(), s.db, identity.PrincipalID)
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	for _, session := range sessions {
		if session.ID == identity.SessionID {
			writeJSON(w, http.StatusOK, identityResponseFrom(identity, session, true))
			return
		}
	}
	writeV1Error(w, r, http.StatusUnauthorized, "invalid_access_token", "Access token is no longer active")
}

func (s *Server) handleLogoutIdentityV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return
	}
	if _, err := auth.RevokeDeviceSession(r.Context(), s.db, identity.PrincipalID, identity.SessionID, "logout"); err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogoutAllIdentityV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return
	}
	if err := auth.RevokeAllDeviceSessions(r.Context(), s.db, identity.PrincipalID, "logout_all"); err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListIdentitySessionsV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return
	}
	sessions, err := auth.ListDeviceSessions(r.Context(), s.db, identity.PrincipalID)
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	items := make([]identitySessionResponse, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, identitySessionFrom(session, session.ID == identity.SessionID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRevokeIdentitySessionV1(w http.ResponseWriter, r *http.Request, sessionID string) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Session ID is required")
		return
	}
	revoked, err := auth.RevokeDeviceSession(r.Context(), s.db, identity.PrincipalID, sessionID, "device_revoked")
	if err != nil {
		s.writeIdentityError(w, r, err)
		return
	}
	if !revoked {
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Device session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) optionalGuestIdentityV1(w http.ResponseWriter, r *http.Request) (*auth.Identity, bool) {
	token, present := bearerToken(r)
	if !present {
		return nil, true
	}
	if token == "" {
		writeV1Error(w, r, http.StatusUnauthorized, "invalid_access_token", "Bearer access token is invalid")
		return nil, false
	}
	identity, err := auth.AuthenticateAccessToken(r.Context(), s.db, s.identityTokens, token)
	if err != nil {
		s.writeIdentityError(w, r, err)
		return nil, false
	}
	if identity.Kind != "guest" {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_guest", "Only a guest identity can be merged")
		return nil, false
	}
	return identity, true
}

func (s *Server) requiredIdentityV1(w http.ResponseWriter, r *http.Request) (*auth.Identity, bool) {
	token, present := bearerToken(r)
	if !present || token == "" {
		writeV1Error(w, r, http.StatusUnauthorized, "invalid_access_token", "Bearer access token is required")
		return nil, false
	}
	identity, err := auth.AuthenticateAccessToken(r.Context(), s.db, s.identityTokens, token)
	if err != nil {
		s.writeIdentityError(w, r, err)
		return nil, false
	}
	return identity, true
}

func decodeIdentityJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	if r.Body == nil {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "A JSON request body is required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIdentityRequestBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeV1Error(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "Request body is too large")
			return false
		}
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Request body must contain one JSON object")
		return false
	}
	return true
}

func (s *Server) writeIdentityError(w http.ResponseWriter, r *http.Request, err error) {
	var httpErr auth.HTTPError
	switch {
	case errors.Is(err, auth.ErrIdentityUnavailable):
		logging.ForRequest("api.v1.identity", r).Warn("identity.unavailable", map[string]any{"status": http.StatusServiceUnavailable})
		writeV1Error(w, r, http.StatusServiceUnavailable, "identity_unavailable", "Mobile identity is not configured")
	case errors.Is(err, auth.ErrAccessTokenExpired):
		writeV1Error(w, r, http.StatusUnauthorized, "access_token_expired", "Access token has expired")
	case errors.Is(err, auth.ErrInvalidAccessToken):
		writeV1Error(w, r, http.StatusUnauthorized, "invalid_access_token", "Access token is invalid")
	case errors.Is(err, auth.ErrInvalidRefreshToken):
		writeV1Error(w, r, http.StatusUnauthorized, "invalid_refresh_token", "Refresh token is invalid")
	case errors.Is(err, auth.ErrRefreshTokenExpired):
		writeV1Error(w, r, http.StatusUnauthorized, "refresh_token_expired", "Refresh token has expired")
	case errors.Is(err, auth.ErrRefreshTokenReused):
		logging.ForRequest("api.v1.identity.refresh", r).Warn("identity.refresh_reuse", map[string]any{"status": http.StatusUnauthorized})
		writeV1Error(w, r, http.StatusUnauthorized, "refresh_token_reused", "Refresh token reuse revoked this device session")
	case errors.Is(err, auth.ErrInvalidGuest):
		writeV1Error(w, r, http.StatusConflict, "invalid_guest", "Guest identity cannot be merged")
	case errors.As(err, &httpErr):
		code := "invalid_request"
		if httpErr.Status == http.StatusUnauthorized {
			code = "invalid_credentials"
		} else if httpErr.Status == http.StatusConflict {
			code = "email_in_use"
		}
		writeV1Error(w, r, httpErr.Status, code, httpErr.Message)
	default:
		logging.ForRequest("api.v1.identity", r).Error("identity.failed", logging.ErrorMeta(err))
		writeV1Error(w, r, http.StatusInternalServerError, "internal_error", "Identity request failed")
	}
}

func identityGrantResponse(grant auth.TokenGrant) identityResponse {
	response := identityResponseFrom(&grant.Identity, grant.Session, true)
	response.Tokens = &identityTokensResponse{
		TokenType: "Bearer", AccessToken: grant.AccessToken, AccessTokenExpiresAt: formatIdentityTime(grant.AccessTokenExpiresAt),
		RefreshToken: grant.RefreshToken, RefreshTokenExpiresAt: formatIdentityTime(grant.RefreshTokenExpiresAt),
	}
	return response
}

func identityResponseFrom(identity *auth.Identity, session auth.DeviceSession, current bool) identityResponse {
	return identityResponse{
		Principal: identityPrincipalResponse{ID: identity.PrincipalID, Type: identity.Kind},
		User:      identity.User,
		Session:   identitySessionFrom(session, current),
	}
}

func identitySessionFrom(session auth.DeviceSession, current bool) identitySessionResponse {
	return identitySessionResponse{
		ID: session.ID, DeviceName: session.DeviceName, Platform: session.Platform, Current: current,
		ExpiresAt: formatIdentityTime(session.ExpiresAt), LastSeenAt: formatIdentityTime(session.LastSeenAt), CreatedAt: formatIdentityTime(session.CreatedAt),
	}
}

func formatIdentityTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
