package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultAPIURL     = "https://api.cartesia.ai/tts/bytes"
	defaultAPIVersion = "2026-08-14"
	defaultModel      = "sonic-3.6"
	maxAudioBytes     = 25 * 1024 * 1024
)

type Synthesizer interface {
	Synthesize(context.Context, string) ([]byte, error)
}

type Config struct {
	APIKey     string
	VoiceID    string
	Model      string
	APIURL     string
	APIVersion string
	HTTPClient *http.Client
}

type cartesiaClient struct {
	config Config
}

type cartesiaRequest struct {
	ModelID          string                   `json:"model_id"`
	Transcript       string                   `json:"transcript"`
	Voice            string                   `json:"voice"`
	OutputFormat     cartesiaOutputFormat     `json:"output_format"`
	Language         string                   `json:"language"`
	Normalization    string                   `json:"normalization"`
	GenerationConfig cartesiaGenerationConfig `json:"generation_config"`
}

type cartesiaOutputFormat struct {
	Container  string `json:"container"`
	SampleRate int    `json:"sample_rate"`
	BitRate    int    `json:"bit_rate"`
}

type cartesiaGenerationConfig struct {
	Volume float64 `json:"volume"`
	Speed  float64 `json:"speed"`
}

func ConfigFromEnv() Config {
	return normalizeConfig(Config{
		APIKey:     os.Getenv("CARTESIA_API_KEY"),
		VoiceID:    os.Getenv("CARTESIA_VOICE_ID"),
		Model:      os.Getenv("CARTESIA_MODEL"),
		APIURL:     os.Getenv("CARTESIA_API_URL"),
		APIVersion: os.Getenv("CARTESIA_API_VERSION"),
	})
}

func NewCartesia(config Config) Synthesizer {
	return &cartesiaClient{config: normalizeConfig(config)}
}

func normalizeConfig(config Config) Config {
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.VoiceID = strings.TrimSpace(config.VoiceID)
	config.Model = strings.TrimSpace(config.Model)
	config.APIURL = strings.TrimSpace(config.APIURL)
	config.APIVersion = strings.TrimSpace(config.APIVersion)
	if config.Model == "" {
		config.Model = defaultModel
	}
	if config.APIURL == "" {
		config.APIURL = defaultAPIURL
	}
	if config.APIVersion == "" {
		config.APIVersion = defaultAPIVersion
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 90 * time.Second}
	}
	return config
}

func (c *cartesiaClient) Synthesize(ctx context.Context, transcript string) ([]byte, error) {
	transcript = strings.TrimSpace(transcript)
	if c.config.APIKey == "" || c.config.VoiceID == "" {
		return nil, errors.New("cartesia is not configured")
	}
	if transcript == "" {
		return nil, errors.New("cartesia transcript is empty")
	}

	payload := cartesiaRequest{
		ModelID:       c.config.Model,
		Transcript:    transcript,
		Voice:         c.config.VoiceID,
		OutputFormat:  cartesiaOutputFormat{Container: "mp3", SampleRate: 44100, BitRate: 128000},
		Language:      "en",
		Normalization: "auto",
		GenerationConfig: cartesiaGenerationConfig{
			Volume: 1,
			Speed:  1,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("cartesia request could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.APIURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, errors.New("cartesia request could not be created")
	}
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Cartesia-Version", c.config.APIVersion)
	req.Header.Set("Content-Type", "application/json")

	response, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return nil, errors.New("cartesia request failed")
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, errors.New("cartesia authentication failed")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("cartesia request failed with status %d", response.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(response.Header.Get("Content-Type"), ";", 2)[0]))
	if !strings.HasPrefix(contentType, "audio/") {
		return nil, errors.New("cartesia returned invalid audio")
	}

	audio, err := io.ReadAll(io.LimitReader(response.Body, maxAudioBytes+1))
	if err != nil {
		return nil, errors.New("cartesia audio could not be read")
	}
	if len(audio) == 0 || len(audio) > maxAudioBytes {
		return nil, errors.New("cartesia returned invalid audio")
	}
	return audio, nil
}
