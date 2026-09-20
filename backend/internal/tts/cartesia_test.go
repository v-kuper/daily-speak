package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCartesiaSynthesizeSendsConfiguredRequest(t *testing.T) {
	var got cartesiaRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer sk_test" {
			t.Fatalf("authorization was not bearer token")
		}
		if r.Header.Get("Cartesia-Version") != "2026-08-14" {
			t.Fatalf("version = %q", r.Header.Get("Cartesia-Version"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content type = %q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3-audio"))
	}))
	defer server.Close()

	client := NewCartesia(Config{
		APIKey:     "sk_test",
		VoiceID:    "female-us",
		Model:      "sonic-3.6",
		APIURL:     server.URL,
		APIVersion: "2026-08-14",
		HTTPClient: server.Client(),
	})
	audio, err := client.Synthesize(context.Background(), "I went to the store.")
	if err != nil || string(audio) != "ID3-audio" {
		t.Fatalf("audio=%q err=%v", audio, err)
	}
	if got.Transcript != "I went to the store." || got.ModelID != "sonic-3.6" || got.Voice != "female-us" {
		t.Fatalf("request=%#v", got)
	}
	if got.Language != "en" || got.Normalization != "auto" || got.OutputFormat.Container != "mp3" {
		t.Fatalf("request=%#v", got)
	}
	if got.OutputFormat.SampleRate != 44100 || got.OutputFormat.BitRate != 128000 {
		t.Fatalf("output format=%#v", got.OutputFormat)
	}
	if got.GenerationConfig.Speed != 1 || got.GenerationConfig.Volume != 1 {
		t.Fatalf("generation config=%#v", got.GenerationConfig)
	}
}

func TestCartesiaSynthesizeRejectsUnsafeResponsesAndMissingConfiguration(t *testing.T) {
	cases := []struct {
		name        string
		apiKey      string
		voiceID     string
		contentType string
		status      int
		body        []byte
	}{
		{"missing api key", "", "female-us", "audio/mpeg", http.StatusOK, []byte("audio")},
		{"missing voice", "sk_test", "", "audio/mpeg", http.StatusOK, []byte("audio")},
		{"provider unauthorized", "sk_test", "female-us", "application/json", http.StatusUnauthorized, []byte(`{"error":"secret detail"}`)},
		{"non audio response", "sk_test", "female-us", "application/json", http.StatusOK, []byte(`{"ok":true}`)},
		{"empty audio", "sk_test", "female-us", "audio/mpeg", http.StatusOK, nil},
		{"oversized audio", "sk_test", "female-us", "audio/mpeg", http.StatusOK, bytes.Repeat([]byte{'a'}, maxAudioBytes+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.body)
			}))
			defer server.Close()

			client := NewCartesia(Config{
				APIKey:     tc.apiKey,
				VoiceID:    tc.voiceID,
				Model:      "sonic-3.6",
				APIURL:     server.URL,
				APIVersion: "2026-08-14",
				HTTPClient: server.Client(),
			})
			audio, err := client.Synthesize(context.Background(), "Practice text")
			if err == nil {
				t.Fatal("expected synthesis error")
			}
			if audio != nil {
				t.Fatalf("expected nil audio, got %d bytes", len(audio))
			}
			message := err.Error()
			if strings.Contains(message, "secret detail") || strings.Contains(message, "sk_test") {
				t.Fatalf("error leaked provider detail or key: %q", message)
			}
		})
	}
}

func TestCartesiaSynthesizeHonorsHTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("late audio"))
	}))
	defer server.Close()

	client := NewCartesia(Config{
		APIKey:     "sk_test",
		VoiceID:    "female-us",
		Model:      "sonic-3.6",
		APIURL:     server.URL,
		APIVersion: "2026-08-14",
		HTTPClient: &http.Client{Timeout: 20 * time.Millisecond},
	})
	audio, err := client.Synthesize(context.Background(), "Practice text")
	if err == nil || audio != nil {
		t.Fatalf("expected timeout error and nil audio, got audio=%q err=%v", audio, err)
	}
}

func TestConfigFromEnvAppliesStableDefaults(t *testing.T) {
	t.Setenv("CARTESIA_API_KEY", " key ")
	t.Setenv("CARTESIA_VOICE_ID", " voice ")
	t.Setenv("CARTESIA_MODEL", "")
	t.Setenv("CARTESIA_API_URL", "")
	t.Setenv("CARTESIA_API_VERSION", "")

	config := ConfigFromEnv()
	if config.APIKey != "key" || config.VoiceID != "voice" {
		t.Fatalf("credentials were not trimmed: %#v", config)
	}
	if config.Model != "sonic-3.6" || config.APIURL != "https://api.cartesia.ai/tts/bytes" || config.APIVersion != "2026-08-14" {
		t.Fatalf("defaults=%#v", config)
	}
	if config.HTTPClient == nil || config.HTTPClient.Timeout != 90*time.Second {
		t.Fatalf("http client=%#v", config.HTTPClient)
	}
}
