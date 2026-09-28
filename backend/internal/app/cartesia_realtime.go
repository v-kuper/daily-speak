package app

import (
	"context"
	"net/url"
	"os"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/interview"
	"daily-speaking-practice/backend/internal/transcription"
)

const (
	cartesiaRealtimeModel      = "ink-2"
	cartesiaRealtimeEncoding   = "pcm_s16le"
	cartesiaRealtimeSampleRate = 16000
	cartesiaRealtimeTokenTTL   = 15 * time.Minute
)

type cartesiaRealtimeIssuer struct {
	client       *transcription.Cartesia
	websocketURL string
	apiVersion   string
	now          func() time.Time
}

func newCartesiaRealtimeIssuer() interview.RealtimeCredentialIssuer {
	apiVersion := strings.TrimSpace(os.Getenv("CARTESIA_API_VERSION"))
	if apiVersion == "" {
		apiVersion = "2026-08-14"
	}
	websocketURL := strings.TrimSpace(os.Getenv("CARTESIA_STT_WEBSOCKET_URL"))
	if websocketURL == "" {
		websocketURL = "wss://api.cartesia.ai/stt/websocket"
	}
	return &cartesiaRealtimeIssuer{
		client: transcription.NewCartesia(transcription.CartesiaConfig{
			APIKey:     os.Getenv("CARTESIA_API_KEY"),
			APIVersion: apiVersion,
		}),
		websocketURL: websocketURL,
		apiVersion:   apiVersion,
		now:          time.Now,
	}
}

func (i *cartesiaRealtimeIssuer) IssueRealtimeCredential(ctx context.Context, ttl time.Duration) (interview.RealtimeTranscriptionCredential, error) {
	if ttl > cartesiaRealtimeTokenTTL {
		ttl = cartesiaRealtimeTokenTTL
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
	query.Set("model", cartesiaRealtimeModel)
	query.Set("encoding", cartesiaRealtimeEncoding)
	query.Set("sample_rate", "16000")
	query.Set("cartesia_version", i.apiVersion)
	endpoint.RawQuery = query.Encode()
	return interview.RealtimeTranscriptionCredential{
		Token: token, ExpiresAt: issuedAt.Add(ttl),
		WebSocketURL: endpoint.String(), Model: cartesiaRealtimeModel,
		Encoding: cartesiaRealtimeEncoding, SampleRate: cartesiaRealtimeSampleRate,
	}, nil
}
