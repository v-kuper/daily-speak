package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCartesiaTranscribeSendsAudioAndReturnsText(t *testing.T) {
	audio := filepath.Join(t.TempDir(), "answer.webm")
	if err := os.WriteFile(audio, []byte("audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/stt" {
			t.Errorf("unexpected Cartesia request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Cartesia-Version") != cartesiaDefaultAPIVersion {
			t.Errorf("unexpected authentication headers")
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "audio bytes" || header.Filename != "answer.webm" || r.FormValue("model") != cartesiaDefaultModel || r.FormValue("language") != "en" {
			t.Errorf("unexpected audio or form fields")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "transcript", "text": " Hello   world. ", "duration": 1.2,
		})
	}))
	defer server.Close()

	client := NewCartesia(CartesiaConfig{APIKey: "test-key", Language: "en", STTEndpoint: server.URL + "/stt"})
	text, err := client.Transcribe(context.Background(), audio)
	if err != nil || text != "Hello world." {
		t.Fatalf("unexpected transcript %q, error %v", text, err)
	}
}

func TestCartesiaTranscriptionErrorsAreSanitized(t *testing.T) {
	audio := filepath.Join(t.TempDir(), "answer.wav")
	if err := os.WriteFile(audio, []byte("audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"sensitive transcript or token"}`)
	}))
	defer server.Close()

	_, err := NewCartesia(CartesiaConfig{APIKey: "test-key", STTEndpoint: server.URL}).Transcribe(context.Background(), audio)
	var typed Error
	if !errors.As(err, &typed) || typed.Status != http.StatusTooManyRequests || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestCartesiaRejectsMissingKeyAndInvalidTranscript(t *testing.T) {
	_, err := NewCartesia(CartesiaConfig{}).Transcribe(context.Background(), "missing.wav")
	var typed Error
	if !errors.As(err, &typed) || !strings.Contains(typed.Message, "CARTESIA_API_KEY") {
		t.Fatalf("expected missing key error, got %v", err)
	}

	audio := filepath.Join(t.TempDir(), "answer.wav")
	if err := os.WriteFile(audio, []byte("audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"type":"transcript","text":"   "}`)
	}))
	defer server.Close()
	_, err = NewCartesia(CartesiaConfig{APIKey: "test-key", STTEndpoint: server.URL}).Transcribe(context.Background(), audio)
	if !errors.As(err, &typed) || typed.Status != http.StatusBadGateway {
		t.Fatalf("expected invalid transcript error, got %v", err)
	}
}

func TestCartesiaCreatesSTTOnlyAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/access-token" {
			t.Errorf("unexpected access token request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Cartesia-Version") != "2026-08-14" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected access token headers")
		}
		var body struct {
			Grants struct {
				STT bool `json:"stt"`
				TTS bool `json:"tts"`
			} `json:"grants"`
			ExpiresIn int `json:"expires_in"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !body.Grants.STT || body.Grants.TTS || body.ExpiresIn != 900 {
			t.Errorf("unexpected access token payload: %#v", body)
		}
		_, _ = io.WriteString(w, `{"token":"short-lived-stt-token"}`)
	}))
	defer server.Close()

	token, err := NewCartesia(CartesiaConfig{
		APIKey: "test-key", AccessTokenEndpoint: server.URL + "/access-token",
	}).CreateSTTAccessToken(context.Background(), 15*time.Minute)
	if err != nil || token != "short-lived-stt-token" {
		t.Fatalf("unexpected token %q, error %v", token, err)
	}
}

func TestCartesiaAccessTokenValidationAndErrorsAreSanitized(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "sensitive provider response")
	}))
	defer server.Close()
	cartesia := NewCartesia(CartesiaConfig{APIKey: "test-key", AccessTokenEndpoint: server.URL})

	for _, ttl := range []time.Duration{0, time.Second / 2, MaxAccessTokenTTL + time.Second} {
		if _, err := cartesia.CreateSTTAccessToken(context.Background(), ttl); !errors.As(err, new(Error)) {
			t.Fatalf("expected local TTL validation for %s, got %v", ttl, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid TTL made %d provider requests", requests.Load())
	}

	_, err := cartesia.CreateSTTAccessToken(context.Background(), time.Minute)
	var typed Error
	if !errors.As(err, &typed) || typed.Status != http.StatusUnauthorized || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("expected sanitized authentication error, got %v", err)
	}
}

func TestCartesiaCheckUsesTokenEndpointWithoutAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/access-token" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Errorf("configuration check submitted an unexpected request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"token":"check-token"}`)
	}))
	defer server.Close()
	if err := NewCartesia(CartesiaConfig{APIKey: "test-key", AccessTokenEndpoint: server.URL + "/access-token"}).Check(context.Background()); err != nil {
		t.Fatalf("expected valid Cartesia configuration: %v", err)
	}
}
