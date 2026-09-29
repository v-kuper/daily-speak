package transcription

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cartesiaSTTEndpoint         = "https://api.cartesia.ai/stt"
	cartesiaAccessTokenEndpoint = "https://api.cartesia.ai/access-token"
	cartesiaDefaultModel        = "ink-whisper"
	cartesiaDefaultAPIVersion   = "2026-08-14"
	cartesiaMaxResponseBytes    = 2 * 1024 * 1024
	cartesiaMaxTokenResponse    = 64 * 1024
	MaxAccessTokenTTL           = time.Hour
)

type CartesiaConfig struct {
	APIKey              string
	Model               string
	Language            string
	APIVersion          string
	Timeout             time.Duration
	STTEndpoint         string
	AccessTokenEndpoint string
	Client              *http.Client
}

type Cartesia struct {
	config CartesiaConfig
}

func NewCartesia(config CartesiaConfig) *Cartesia {
	if config.Model == "" {
		config.Model = cartesiaDefaultModel
	}
	if config.APIVersion == "" {
		config.APIVersion = cartesiaDefaultAPIVersion
	}
	if config.STTEndpoint == "" {
		config.STTEndpoint = cartesiaSTTEndpoint
	}
	if config.AccessTokenEndpoint == "" {
		config.AccessTokenEndpoint = cartesiaAccessTokenEndpoint
	}
	if config.Timeout <= 0 {
		config.Timeout = 3 * time.Minute
	}
	if config.Client == nil {
		config.Client = http.DefaultClient
	}
	return &Cartesia{config: config}
}

// Check verifies the API key and network path by issuing a short-lived STT
// access token. It does not submit audio or consume transcription credits.
func (c *Cartesia) Check(ctx context.Context) error {
	_, err := c.CreateSTTAccessToken(ctx, time.Minute)
	return err
}

// CreateSTTAccessToken creates a browser-safe token that grants only STT
// access. Cartesia does not return token metadata, so callers must track the
// requested lifetime themselves.
func (c *Cartesia) CreateSTTAccessToken(ctx context.Context, ttl time.Duration) (string, error) {
	return c.createAccessToken(ctx, ttl, false, true)
}

// CreateTTSAccessToken creates a browser-safe token that grants only TTS
// access. Keeping the grants separate prevents a leaked playback credential
// from opening another transcription stream.
func (c *Cartesia) CreateTTSAccessToken(ctx context.Context, ttl time.Duration) (string, error) {
	return c.createAccessToken(ctx, ttl, true, false)
}

func (c *Cartesia) createAccessToken(ctx context.Context, ttl time.Duration, ttsGrant, sttGrant bool) (string, error) {
	if err := c.validateKey(); err != nil {
		return "", err
	}
	if ttl <= 0 || ttl > MaxAccessTokenTTL || ttl%time.Second != 0 {
		return "", Error{Message: "Cartesia access token lifetime must be a whole number of seconds between 1 and 3600.", Status: http.StatusBadRequest}
	}
	payload, err := json.Marshal(struct {
		Grants struct {
			TTS bool `json:"tts"`
			STT bool `json:"stt"`
		} `json:"grants"`
		ExpiresIn int64 `json:"expires_in"`
	}{
		Grants: struct {
			TTS bool `json:"tts"`
			STT bool `json:"stt"`
		}{TTS: ttsGrant, STT: sttGrant},
		ExpiresIn: int64(ttl / time.Second),
	})
	if err != nil {
		return "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, min(c.config.Timeout, 15*time.Second))
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.config.AccessTokenEndpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	c.setHeaders(request)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.config.Client.Do(request)
	if err != nil {
		return "", fmt.Errorf("Cartesia access token request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", providerError("Cartesia access token request", response.StatusCode)
	}
	data, err := readBounded(response.Body, cartesiaMaxTokenResponse, "Cartesia access token response")
	if err != nil {
		return "", err
	}
	var result struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(data, &result) != nil || strings.TrimSpace(result.Token) == "" {
		return "", Error{Message: "Cartesia returned an invalid access token response.", Status: http.StatusBadGateway}
	}
	return result.Token, nil
}

func (c *Cartesia) Transcribe(ctx context.Context, path string) (string, error) {
	if err := c.validateKey(); err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", Error{Message: "Audio file path is required for transcription.", Status: http.StatusInternalServerError}
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	requestCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.config.STTEndpoint, reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return "", err
	}
	defer reader.Close()
	c.setHeaders(request)
	request.Header.Set("Content-Type", form.FormDataContentType())
	go func() {
		writeErr := writeCartesiaMultipart(form, file, filepath.Base(path), c.config.Model, c.config.Language)
		_ = writer.CloseWithError(writeErr)
	}()

	response, err := c.config.Client.Do(request)
	if err != nil {
		return "", fmt.Errorf("Cartesia transcription request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", providerError("Cartesia transcription", response.StatusCode)
	}
	data, err := readBounded(response.Body, cartesiaMaxResponseBytes, "Cartesia transcription response")
	if err != nil {
		return "", err
	}
	var result struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(data, &result) != nil || normalizeTranscript(result.Text) == "" {
		return "", Error{Message: "Cartesia returned an empty or invalid transcript.", Status: http.StatusBadGateway}
	}
	return normalizeTranscript(result.Text), nil
}

func (c *Cartesia) validateKey() error {
	if strings.TrimSpace(c.config.APIKey) == "" {
		return Error{Message: "CARTESIA_API_KEY is required for transcription.", Status: http.StatusInternalServerError}
	}
	return nil
}

func (c *Cartesia) setHeaders(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	request.Header.Set("Cartesia-Version", c.config.APIVersion)
}

func writeCartesiaMultipart(form *multipart.Writer, file *os.File, filename, model, language string) error {
	part, err := form.CreateFormFile("file", filename)
	if err == nil {
		_, err = io.Copy(part, file)
	}
	if err == nil {
		err = form.WriteField("model", model)
	}
	if normalized := normalizeLanguage(language); err == nil && normalized != "" {
		err = form.WriteField("language", normalized)
	}
	if closeErr := form.Close(); err == nil {
		err = closeErr
	}
	return err
}

func readBounded(reader io.Reader, limit int64, label string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, Error{Message: label + " is too large.", Status: http.StatusBadGateway}
	}
	return data, nil
}

func providerError(operation string, status int) Error {
	return Error{Message: fmt.Sprintf("%s failed (HTTP %d).", operation, status), Status: status}
}
