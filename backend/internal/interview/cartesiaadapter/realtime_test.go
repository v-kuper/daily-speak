package cartesiaadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestIssuerMapsCartesiaProtocolToInterviewCredential(t *testing.T) {
	fixedNow := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	var requestedTTL int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected authorization header")
		}
		if r.Header.Get("Cartesia-Version") != "test-version" {
			t.Errorf("unexpected Cartesia version")
		}
		var payload struct {
			ExpiresIn int64 `json:"expires_in"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requestedTTL = payload.ExpiresIn
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"scoped-token"}`))
	}))
	defer server.Close()

	issuer := New(Config{
		APIKey: "secret", APIVersion: "test-version",
		WebSocketURL:        "wss://speech.example.test/realtime?region=test",
		AccessTokenEndpoint: server.URL, HTTPClient: server.Client(),
		Now: func() time.Time { return fixedNow },
	})
	credential, err := issuer.IssueRealtimeCredential(context.Background(), 30*time.Minute)
	if err != nil {
		t.Fatalf("issue realtime credential: %v", err)
	}
	if requestedTTL != 900 {
		t.Fatalf("requested TTL = %d, want 900", requestedTTL)
	}
	if credential.Token != "scoped-token" || !credential.ExpiresAt.Equal(fixedNow.Add(15*time.Minute)) {
		t.Fatalf("unexpected credential metadata: %+v", credential)
	}
	endpoint, err := url.Parse(credential.WebSocketURL)
	if err != nil {
		t.Fatalf("parse WebSocket URL: %v", err)
	}
	query := endpoint.Query()
	if query.Get("region") != "test" || query.Get("model") != "ink-2" ||
		query.Get("encoding") != "pcm_s16le" || query.Get("sample_rate") != "16000" ||
		query.Get("cartesia_version") != "test-version" {
		t.Fatalf("unexpected WebSocket query: %v", query)
	}
	if credential.Model != "ink-2" || credential.Encoding != "pcm_s16le" || credential.SampleRate != 16000 {
		t.Fatalf("unexpected audio contract: %+v", credential)
	}
}

func TestIssuerCreatesTTSOnlyQuestionSpeechCredential(t *testing.T) {
	fixedNow := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Grants struct {
				TTS bool `json:"tts"`
				STT bool `json:"stt"`
			} `json:"grants"`
			ExpiresIn int64 `json:"expires_in"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode token request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !payload.Grants.TTS || payload.Grants.STT || payload.ExpiresIn != 60 {
			t.Fatalf("unexpected grant payload: %+v", payload)
		}
		_, _ = w.Write([]byte(`{"token":"tts-token"}`))
	}))
	defer server.Close()

	issuer := New(Config{
		APIKey: "secret", APIVersion: "test-version", AccessTokenEndpoint: server.URL,
		TTSAPIURL: "https://speech.example.test/tts/bytes", TTSModel: "sonic-test", VoiceID: "voice-test",
		HTTPClient: server.Client(), Now: func() time.Time { return fixedNow },
	})
	credential, err := issuer.IssueQuestionSpeechCredential(context.Background(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Token != "tts-token" || credential.Endpoint != "https://speech.example.test/tts/bytes" ||
		credential.APIVersion != "test-version" || credential.Model != "sonic-test" || credential.VoiceID != "voice-test" ||
		!credential.ExpiresAt.Equal(fixedNow.Add(time.Minute)) {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}
