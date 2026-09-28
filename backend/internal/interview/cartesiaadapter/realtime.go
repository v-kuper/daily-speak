package cartesiaadapter

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/interview"
	"daily-speaking-practice/backend/internal/transcription"
)

const (
	defaultAPIVersion   = "2026-08-14"
	defaultWebSocketURL = "wss://api.cartesia.ai/stt/websocket"
	realtimeModel       = "ink-2"
	realtimeEncoding    = "pcm_s16le"
	realtimeSampleRate  = 16000
	maxTokenTTL         = 15 * time.Minute
)

type Config struct {
	APIKey              string
	APIVersion          string
	WebSocketURL        string
	AccessTokenEndpoint string
	HTTPClient          *http.Client
	Now                 func() time.Time
}

type Issuer struct {
	client       *transcription.Cartesia
	websocketURL string
	apiVersion   string
	now          func() time.Time
}

func New(config Config) *Issuer {
	apiVersion := strings.TrimSpace(config.APIVersion)
	if apiVersion == "" {
		apiVersion = defaultAPIVersion
	}
	websocketURL := strings.TrimSpace(config.WebSocketURL)
	if websocketURL == "" {
		websocketURL = defaultWebSocketURL
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Issuer{
		client: transcription.NewCartesia(transcription.CartesiaConfig{
			APIKey:              config.APIKey,
			APIVersion:          apiVersion,
			AccessTokenEndpoint: config.AccessTokenEndpoint,
			Client:              config.HTTPClient,
		}),
		websocketURL: websocketURL,
		apiVersion:   apiVersion,
		now:          now,
	}
}

func (i *Issuer) IssueRealtimeCredential(ctx context.Context, ttl time.Duration) (interview.RealtimeTranscriptionCredential, error) {
	if ttl > maxTokenTTL {
		ttl = maxTokenTTL
	}
	issuedAt := i.now().UTC()
	token, err := i.client.CreateSTTAccessToken(ctx, ttl)
	if err != nil {
		return interview.RealtimeTranscriptionCredential{}, err
	}
	endpoint, err := url.Parse(i.websocketURL)
	if err != nil {
		return interview.RealtimeTranscriptionCredential{}, err
	}
	query := endpoint.Query()
	query.Set("model", realtimeModel)
	query.Set("encoding", realtimeEncoding)
	query.Set("sample_rate", "16000")
	query.Set("cartesia_version", i.apiVersion)
	endpoint.RawQuery = query.Encode()
	return interview.RealtimeTranscriptionCredential{
		Token: token, ExpiresAt: issuedAt.Add(ttl),
		WebSocketURL: endpoint.String(), Model: realtimeModel,
		Encoding: realtimeEncoding, SampleRate: realtimeSampleRate,
	}, nil
}

var _ interview.RealtimeCredentialIssuer = (*Issuer)(nil)
