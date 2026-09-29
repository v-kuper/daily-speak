package cartesiaadapter

import (
	"context"
	"errors"
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
	defaultTTSAPIURL    = "https://api.cartesia.ai/tts/bytes"
	defaultTTSModel     = "sonic-3.6"
	realtimeModel       = "ink-2"
	realtimeEncoding    = "pcm_s16le"
	realtimeSampleRate  = 16000
	maxTokenTTL         = 15 * time.Minute
)

type Config struct {
	APIKey              string
	APIVersion          string
	WebSocketURL        string
	TTSAPIURL           string
	TTSModel            string
	VoiceID             string
	AccessTokenEndpoint string
	HTTPClient          *http.Client
	Now                 func() time.Time
}

type Issuer struct {
	client       *transcription.Cartesia
	websocketURL string
	apiVersion   string
	ttsAPIURL    string
	ttsModel     string
	voiceID      string
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
	ttsAPIURL := strings.TrimSpace(config.TTSAPIURL)
	if ttsAPIURL == "" {
		ttsAPIURL = defaultTTSAPIURL
	}
	ttsModel := strings.TrimSpace(config.TTSModel)
	if ttsModel == "" {
		ttsModel = defaultTTSModel
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
		ttsAPIURL:    ttsAPIURL,
		ttsModel:     ttsModel,
		voiceID:      strings.TrimSpace(config.VoiceID),
		now:          now,
	}
}

func (i *Issuer) IssueQuestionSpeechCredential(ctx context.Context, ttl time.Duration) (interview.QuestionSpeechCredential, error) {
	if ttl > maxTokenTTL {
		ttl = maxTokenTTL
	}
	if i.voiceID == "" {
		return interview.QuestionSpeechCredential{}, errors.New("Cartesia question voice is not configured")
	}
	endpoint, err := url.Parse(i.ttsAPIURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return interview.QuestionSpeechCredential{}, errors.New("Cartesia question speech endpoint is invalid")
	}
	issuedAt := i.now().UTC()
	token, err := i.client.CreateTTSAccessToken(ctx, ttl)
	if err != nil {
		return interview.QuestionSpeechCredential{}, err
	}
	return interview.QuestionSpeechCredential{
		Token: token, ExpiresAt: issuedAt.Add(ttl), Endpoint: endpoint.String(),
		APIVersion: i.apiVersion, Model: i.ttsModel, VoiceID: i.voiceID,
	}, nil
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
var _ interview.QuestionSpeechCredentialIssuer = (*Issuer)(nil)
