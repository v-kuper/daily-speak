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
	"runtime"
	"strings"
	"testing"
)

func TestGroqTranscribeSendsAudioAndReturnsText(t *testing.T) {
	audio := filepath.Join(t.TempDir(), "answer.wav")
	if err := os.WriteFile(audio, []byte("audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected Groq request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.ParseMultipartForm(groqMaxAudioBytes + 1024); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "audio bytes" || r.FormValue("model") != groqDefaultModel || r.FormValue("response_format") != "json" || r.FormValue("language") != "en" {
			t.Errorf("unexpected audio or form fields")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": " Hello   world. "})
	}))
	defer server.Close()
	client := NewGroq(GroqConfig{APIKey: "test-key", Language: "en", Endpoint: server.URL + "/audio/transcriptions"})
	text, err := client.Transcribe(context.Background(), audio)
	if err != nil || text != "Hello world." {
		t.Fatalf("unexpected transcript %q, error %v", text, err)
	}
}

func TestGroqTimedTranscriptionPreservesSegmentOffsets(t *testing.T) {
	audio := filepath.Join(t.TempDir(), "answer.wav")
	if err := os.WriteFile(audio, []byte("audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Error(err)
		}
		if r.FormValue("response_format") != "verbose_json" {
			t.Errorf("missing timed response format")
		}
		_, _ = io.WriteString(w, `{"text":" Hello world.","segments":[{"start":0.2,"end":0.5,"text":" Hello"},{"start":0.7,"end":1.2,"text":" world."}]}`)
	}))
	defer server.Close()
	result, err := NewGroq(GroqConfig{APIKey: "test-key", Endpoint: server.URL}).TranscribeTimed(context.Background(), audio)
	if err != nil || result.Text != "Hello world." || len(result.Segments) != 2 || result.Segments[1].StartMS != 700 {
		t.Fatalf("unexpected timed transcript %#v, error %v", result, err)
	}
}

func TestGroqKeepsTranscriptWhenTimingIsInvalid(t *testing.T) {
	result, err := parseGroqTimedJSON([]byte(`{"text":" Hello world.","segments":[{"start":2,"end":1,"text":" Hello world."}]}`))
	if err != nil || result.Text != "Hello world." || len(result.Segments) != 0 {
		t.Fatalf("expected text-only fallback, got %#v, error %v", result, err)
	}
}

func TestGroqDoesNotExposeProviderErrorBody(t *testing.T) {
	audio := filepath.Join(t.TempDir(), "answer.wav")
	if err := os.WriteFile(audio, []byte("audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "sensitive transcript or token")
	}))
	defer server.Close()
	_, err := NewGroq(GroqConfig{APIKey: "test-key", Endpoint: server.URL}).Transcribe(context.Background(), audio)
	var typed Error
	if !errors.As(err, &typed) || typed.Status != http.StatusTooManyRequests || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestGroqNeedsKeyBeforeSendingAudio(t *testing.T) {
	_, err := NewGroq(GroqConfig{}).Transcribe(context.Background(), "missing.wav")
	var typed Error
	if !errors.As(err, &typed) || !strings.Contains(typed.Message, "GROQ_API_KEY") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestGroqCheckValidatesKeyAndModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected model check request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"whisper-large-v3-turbo"}]}`)
	}))
	defer server.Close()
	groq := NewGroq(GroqConfig{APIKey: "test-key", ModelsEndpoint: server.URL + "/models"})
	if err := groq.Check(context.Background()); err != nil {
		t.Fatalf("expected valid Groq configuration: %v", err)
	}
}

func TestGroqCheckRejectsUnavailableModelAndSanitizesProviderError(t *testing.T) {
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"other-model"}]}`)
	}))
	defer modelServer.Close()
	groq := NewGroq(GroqConfig{APIKey: "test-key", ModelsEndpoint: modelServer.URL})
	var typed Error
	if err := groq.Check(context.Background()); !errors.As(err, &typed) || !strings.Contains(typed.Message, "unavailable") {
		t.Fatalf("expected unavailable model error, got %v", err)
	}
	unauthorizedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "sensitive provider response")
	}))
	defer unauthorizedServer.Close()
	groq = NewGroq(GroqConfig{APIKey: "test-key", ModelsEndpoint: unauthorizedServer.URL})
	if err := groq.Check(context.Background()); !errors.As(err, &typed) || typed.Status != http.StatusUnauthorized || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("expected sanitized authentication error, got %v", err)
	}
}

func TestGroqCompressesAudioAboveFreePlanLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test ffmpeg replacement is a POSIX script")
	}
	audio := filepath.Join(t.TempDir(), "long.wav")
	if err := os.WriteFile(audio, make([]byte, groqMaxAudioBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nfor arg do output=$arg; done\nprintf compressed > \"$output\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if !strings.HasSuffix(header.Filename, ".flac") || string(data) != "compressed" {
			t.Errorf("expected compressed FLAC upload, got %q (%d bytes)", header.Filename, len(data))
		}
		_, _ = io.WriteString(w, `{"text":"Done."}`)
	}))
	defer server.Close()
	text, err := NewGroq(GroqConfig{APIKey: "test-key", FFmpegPath: ffmpeg, Endpoint: server.URL}).Transcribe(context.Background(), audio)
	if err != nil || text != "Done." {
		t.Fatalf("unexpected compressed transcript %q, error %v", text, err)
	}
}
