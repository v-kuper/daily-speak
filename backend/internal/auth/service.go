package auth

import (
	"context"

	"daily-speaking-practice/backend/internal/db"
)

// IdentityService is the application boundary shared by browser and native
// identity flows. It owns persistence and token configuration, leaving HTTP
// transport responsible only for validation and response mapping.
type IdentityService struct {
	database *db.DB
	tokens   TokenConfig
}

func NewIdentityService(database *db.DB, tokens TokenConfig) *IdentityService {
	return &IdentityService{database: database, tokens: tokens}
}

func (s *IdentityService) CreateAnonymous(ctx context.Context, device DeviceInfo) (TokenGrant, error) {
	return CreateAnonymousIdentity(ctx, s.database, s.tokens, device)
}

func (s *IdentityService) Register(ctx context.Context, credentials Credentials, guest *Identity, device DeviceInfo) (TokenGrant, error) {
	return RegisterIdentityUser(ctx, s.database, s.tokens, credentials, guest, device)
}

func (s *IdentityService) Login(ctx context.Context, credentials Credentials, guest *Identity, device DeviceInfo) (TokenGrant, error) {
	return LoginIdentityUser(ctx, s.database, s.tokens, credentials, guest, device)
}

func (s *IdentityService) Refresh(ctx context.Context, refreshToken string) (TokenGrant, error) {
	return RotateRefreshToken(ctx, s.database, s.tokens, refreshToken)
}

func (s *IdentityService) Authenticate(ctx context.Context, accessToken string) (*Identity, error) {
	return AuthenticateAccessToken(ctx, s.database, s.tokens, accessToken)
}

func (s *IdentityService) ListSessions(ctx context.Context, principalID string) ([]DeviceSession, error) {
	return ListDeviceSessions(ctx, s.database, principalID)
}

func (s *IdentityService) RevokeSession(ctx context.Context, principalID string, sessionID string, reason string) (bool, error) {
	return RevokeDeviceSession(ctx, s.database, principalID, sessionID, reason)
}

func (s *IdentityService) RevokeAllSessions(ctx context.Context, principalID string, reason string) error {
	return RevokeAllDeviceSessions(ctx, s.database, principalID, reason)
}
