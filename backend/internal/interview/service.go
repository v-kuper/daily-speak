package interview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/learner"
	"daily-speaking-practice/backend/internal/quota"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

type Repository interface {
	FindByCreateKey(context.Context, string, string, string) (Session, bool, error)
	EnsureRefill(context.Context, string, string) error
	Create(context.Context, CreateInput, int) (Session, error)
	Get(context.Context, string, string) (Session, error)
	Start(context.Context, string, string) (Session, error)
	Cancel(context.Context, string, string) (Session, error)
	Advance(context.Context, AdvanceInput) (Session, error)
	AttachAudio(context.Context, AttachAudioInput) (Session, error)
	SaveTurnTranscript(context.Context, SaveTurnTranscriptInput) (Session, error)
	Finalize(context.Context, FinalizeInput) (Session, error)
}

type Service struct {
	repository       Repository
	credentialIssuer RealtimeCredentialIssuer
}

func NewService(repository Repository, issuer ...RealtimeCredentialIssuer) *Service {
	service := &Service{repository: repository}
	if len(issuer) > 0 {
		service.credentialIssuer = issuer[0]
	}
	return service
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Session, error) {
	input.Topic = strings.TrimSpace(input.Topic)
	input.OpeningQuestion = strings.TrimSpace(input.OpeningQuestion)
	input.OwnerKind = strings.ToLower(strings.TrimSpace(input.OwnerKind))
	if !keyPattern.MatchString(input.IdempotencyKey) || input.OwnerPrincipalID == "" ||
		len([]rune(input.Topic)) < 2 || len([]rune(input.Topic)) > 300 ||
		len([]rune(input.OpeningQuestion)) > 300 {
		return Session{}, ErrInvalid
	}
	if input.OwnerKind != "guest" && input.OwnerKind != "user" ||
		input.OwnerKind == "user" && strings.TrimSpace(input.UserID) == "" ||
		input.OwnerKind == "guest" && strings.TrimSpace(input.UserID) != "" {
		return Session{}, ErrInvalid
	}
	if !strings.Contains(input.OpeningQuestion, "?") {
		subject := input.Topic
		if input.OpeningQuestion != "" {
			subject = input.OpeningQuestion
		}
		input.OpeningQuestion = "What would you like to share about " + truncateRunes(strings.TrimRight(subject, ".!? "), 230) + "?"
	}
	if len([]rune(input.OpeningQuestion)) < 8 || len([]rune(input.OpeningQuestion)) > 300 {
		return Session{}, ErrInvalid
	}
	input.EnglishLevel = learner.NormalizeEnglishLevel(input.EnglishLevel)
	input.Interests = learner.NormalizeInterests(input.Interests, 10)
	digestData, _ := json.Marshal([]any{input.Topic, input.OpeningQuestion, input.EnglishLevel, input.Interests})
	digest := sha256.Sum256(digestData)
	input.RequestDigest = hex.EncodeToString(digest[:])
	if existing, found, err := s.repository.FindByCreateKey(ctx, input.OwnerPrincipalID, input.IdempotencyKey, input.RequestDigest); err != nil {
		return Session{}, err
	} else if found {
		return existing, nil
	}
	maxSeconds := quota.GuestMaxSessionSeconds
	if input.OwnerKind == "user" {
		maxSeconds = quota.AccountMaxSessionSeconds
	}
	return s.repository.Create(ctx, input, maxSeconds)
}

func (s *Service) Get(ctx context.Context, ownerPrincipalID, sessionID string) (Session, error) {
	session, err := s.repository.Get(ctx, ownerPrincipalID, sessionID)
	if err != nil {
		return Session{}, err
	}
	if session.Status == StatusRecording && len(session.Candidates) <= 1 {
		if err := s.repository.EnsureRefill(ctx, ownerPrincipalID, sessionID); err != nil {
			return Session{}, err
		}
	}
	return session, nil
}

func (s *Service) Start(ctx context.Context, ownerPrincipalID, sessionID string) (Session, error) {
	return s.repository.Start(ctx, ownerPrincipalID, sessionID)
}

func (s *Service) Cancel(ctx context.Context, ownerPrincipalID, sessionID string) (Session, error) {
	return s.repository.Cancel(ctx, ownerPrincipalID, sessionID)
}

func (s *Service) Advance(ctx context.Context, input AdvanceInput) (Session, error) {
	if !keyPattern.MatchString(input.IdempotencyKey) || input.CurrentTurnSeq < 1 || input.NextCandidateID == "" || input.AtMs < 1 {
		return Session{}, ErrInvalid
	}
	return s.repository.Advance(ctx, input)
}

func (s *Service) AttachAudio(ctx context.Context, input AttachAudioInput) (Session, error) {
	if !keyPattern.MatchString(input.IdempotencyKey) || input.TurnSeq < 1 || input.AudioAssetID == "" {
		return Session{}, ErrInvalid
	}
	return s.repository.AttachAudio(ctx, input)
}

func (s *Service) SaveTurnTranscript(ctx context.Context, input SaveTurnTranscriptInput) (Session, error) {
	if !keyPattern.MatchString(input.IdempotencyKey) || input.TurnSeq < 1 {
		return Session{}, ErrInvalid
	}
	input.Transcript = strings.Join(strings.Fields(input.Transcript), " ")
	if input.Transcript == "" || len([]rune(input.Transcript)) > 4000 {
		return Session{}, ErrInvalid
	}
	return s.repository.SaveTurnTranscript(ctx, input)
}

func (s *Service) RealtimeTranscriptionCredential(ctx context.Context, ownerPrincipalID, sessionID string) (RealtimeTranscriptionCredential, error) {
	if strings.TrimSpace(ownerPrincipalID) == "" || strings.TrimSpace(sessionID) == "" {
		return RealtimeTranscriptionCredential{}, ErrInvalid
	}
	session, err := s.repository.Get(ctx, ownerPrincipalID, sessionID)
	if err != nil {
		return RealtimeTranscriptionCredential{}, err
	}
	if s.credentialIssuer == nil {
		return RealtimeTranscriptionCredential{}, ErrUnavailable
	}
	// Credential issuance is the first server-side action after the browser has
	// acquired the microphone. Activating a ready session here prevents STT
	// credentials from being minted indefinitely without consuming its duration.
	if session.Status == StatusReady {
		session, err = s.repository.Start(ctx, ownerPrincipalID, sessionID)
		if err != nil {
			return RealtimeTranscriptionCredential{}, err
		}
	}
	if session.Status != StatusRecording || session.StartedAt == nil || session.MaxDurationSeconds <= 0 {
		return RealtimeTranscriptionCredential{}, ErrConflict
	}
	now := time.Now().UTC()
	deadline := session.StartedAt.UTC().Add(time.Duration(session.MaxDurationSeconds) * time.Second)
	if !session.ExpiresAt.IsZero() && session.ExpiresAt.Before(deadline) {
		deadline = session.ExpiresAt
	}
	ttl := deadline.Sub(now).Truncate(time.Second)
	if ttl < time.Second {
		return RealtimeTranscriptionCredential{}, ErrDurationLimit
	}
	credential, err := s.credentialIssuer.IssueRealtimeCredential(ctx, ttl)
	if err != nil || strings.TrimSpace(credential.Token) == "" {
		return RealtimeTranscriptionCredential{}, ErrUnavailable
	}
	return credential, nil
}

func (s *Service) Finalize(ctx context.Context, input FinalizeInput) (Session, error) {
	if !keyPattern.MatchString(input.IdempotencyKey) || input.EndedAtMs <= 0 ||
		(input.RecordingID == "") == (input.GuestPreviewID == "") {
		return Session{}, ErrInvalid
	}
	return s.repository.Finalize(ctx, input)
}
