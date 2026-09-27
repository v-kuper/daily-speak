package auth

import (
	"context"

	"daily-speaking-practice/backend/internal/db"
)

// MobileService is the application boundary for native-client identity flows.
// It owns the persistence and token configuration required by those use cases,
// leaving HTTP transport responsible only for validation and response mapping.
type MobileService struct {
	database *db.DB
	tokens   TokenConfig
}

func NewMobileService(database *db.DB, tokens TokenConfig) *MobileService {
	return &MobileService{database: database, tokens: tokens}
}

func (s *MobileService) CreateAnonymous(ctx context.Context, device DeviceInfo) (TokenGrant, error) {
	return CreateAnonymousIdentity(ctx, s.database, s.tokens, device)
}

func (s *MobileService) Register(ctx context.Context, credentials Credentials, guest *Identity, device DeviceInfo) (TokenGrant, error) {
	return RegisterMobileUser(ctx, s.database, s.tokens, credentials, guest, device)
}

func (s *MobileService) Login(ctx context.Context, credentials Credentials, guest *Identity, device DeviceInfo) (TokenGrant, error) {
	return LoginMobileUser(ctx, s.database, s.tokens, credentials, guest, device)
}

func (s *MobileService) Refresh(ctx context.Context, refreshToken string) (TokenGrant, error) {
	return RotateRefreshToken(ctx, s.database, s.tokens, refreshToken)
}

func (s *MobileService) Authenticate(ctx context.Context, accessToken string) (*Identity, error) {
	return AuthenticateAccessToken(ctx, s.database, s.tokens, accessToken)
}

func (s *MobileService) ListSessions(ctx context.Context, principalID string) ([]DeviceSession, error) {
	return ListDeviceSessions(ctx, s.database, principalID)
}

func (s *MobileService) RevokeSession(ctx context.Context, principalID string, sessionID string, reason string) (bool, error) {
	return RevokeDeviceSession(ctx, s.database, principalID, sessionID, reason)
}

func (s *MobileService) RevokeAllSessions(ctx context.Context, principalID string, reason string) error {
	return RevokeAllDeviceSessions(ctx, s.database, principalID, reason)
}
