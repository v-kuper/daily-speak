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
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	groqEndpoint       = "https://api.groq.com/openai/v1/audio/transcriptions"
	groqModelsEndpoint = "https://api.groq.com/openai/v1/models"
	groqDefaultModel   = "whisper-large-v3-turbo"
	groqMaxAudioBytes  = 25_000_000
	groqMaxResponse    = 2 * 1024 * 1024
)

type GroqConfig struct {
	APIKey         string
	Model          string
	Language       string
	Prompt         string
	FFmpegPath     string
	Timeout        time.Duration
	Endpoint       string
	ModelsEndpoint string
	Client         *http.Client
}

type Groq struct {
	config GroqConfig
}

func NewGroq(config GroqConfig) *Groq {
	if config.Model == "" {
		config.Model = groqDefaultModel
	}
	if config.Endpoint == "" {
		config.Endpoint = groqEndpoint
	}
	if config.ModelsEndpoint == "" {
		config.ModelsEndpoint = groqModelsEndpoint
	}
	if config.Timeout <= 0 {
		config.Timeout = 3 * time.Minute
	}
	if config.Client == nil {
		config.Client = http.DefaultClient
	}
	return &Groq{config: config}
}

// Check verifies the configured key and model without submitting audio.
func (g *Groq) Check(ctx context.Context) error {
	if strings.TrimSpace(g.config.APIKey) == "" {
		return Error{Message: "GROQ_API_KEY is required for transcription.", Status: 500}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(checkCtx, http.MethodGet, g.config.ModelsEndpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+g.config.APIKey)
	response, err := g.config.Client.Do(request)
	if err != nil {
		return fmt.Errorf("Groq model check failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Error{Message: fmt.Sprintf("Groq model check failed (HTTP %d).", response.StatusCode), Status: response.StatusCode}
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, groqMaxResponse)).Decode(&models); err != nil {
		return Error{Message: "Groq returned an invalid model list.", Status: 502}
	}
	for _, model := range models.Data {
		if model.ID == g.config.Model {
			return nil
		}
	}
	return Error{Message: "Configured Groq transcription model is unavailable.", Status: 502}
}

func (g *Groq) Transcribe(ctx context.Context, path string) (string, error) {
	data, err := g.request(ctx, path, "json")
	if err != nil {
		return "", err
	}
	var result struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(data, &result) != nil || normalizeTranscript(result.Text) == "" {
		return "", Error{Message: "Groq returned an empty or invalid transcript.", Status: 502}
	}
	return normalizeTranscript(result.Text), nil
}

func (g *Groq) TranscribeTimed(ctx context.Context, path string) (TimedResult, error) {
	data, err := g.request(ctx, path, "verbose_json")
	if err != nil {
		return TimedResult{}, err
	}
	result, err := parseGroqTimedJSON(data)
	if err != nil {
		return TimedResult{}, Error{Message: "Groq returned invalid timed transcription data.", Status: 502}
	}
	return result, nil
}

func (g *Groq) request(ctx context.Context, path, responseFormat string) ([]byte, error) {
	if strings.TrimSpace(g.config.APIKey) == "" {
		return nil, Error{Message: "GROQ_API_KEY is required for transcription.", Status: 500}
	}
	if strings.TrimSpace(path) == "" {
		return nil, Error{Message: "Audio file path is required for transcription.", Status: 500}
	}
	requestCtx, cancel := context.WithTimeout(ctx, g.config.Timeout)
	defer cancel()
	preparedPath, cleanup, err := g.prepareAudio(requestCtx, path)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	file, err := os.Open(preparedPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filepath.Base(preparedPath))
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(part, io.LimitReader(file, groqMaxAudioBytes+1)); err != nil {
		return nil, err
	}
	fields := map[string]string{
		"model": g.config.Model, "response_format": responseFormat,
	}
	if language := normalizeLanguage(g.config.Language); language != "" {
		fields["language"] = language
	}
	if prompt := strings.TrimSpace(g.config.Prompt); prompt != "" {
		fields["prompt"] = truncate(prompt, 500)
	}
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			return nil, err
		}
	}
	if err := form.Close(); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, g.config.Endpoint, &body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+g.config.APIKey)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response, err := g.config.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Groq transcription request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, Error{Message: fmt.Sprintf("Groq transcription failed (HTTP %d).", response.StatusCode), Status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, groqMaxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(data) > groqMaxResponse {
		return nil, Error{Message: "Groq transcription response is too large.", Status: 502}
	}
	return data, nil
}

func (g *Groq) prepareAudio(ctx context.Context, path string) (string, func(), error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}
	if info.Size() <= groqMaxAudioBytes {
		return path, func() {}, nil
	}
	ffmpeg := g.config.FFmpegPath
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	output, err := os.CreateTemp("", "daily-groq-*.flac")
	if err != nil {
		return "", nil, err
	}
	outputPath := output.Name()
	_ = output.Close()
	cleanup := func() { _ = os.Remove(outputPath) }
	command := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-i", path, "-map", "0:a:0", "-ac", "1", "-ar", "16000", "-c:a", "flac", "-y", outputPath)
	if err := command.Run(); err != nil {
		cleanup()
		return "", nil, Error{Message: "Could not compress audio for Groq transcription.", Status: 502}
	}
	compressed, err := os.Stat(outputPath)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if compressed.Size() == 0 || compressed.Size() > groqMaxAudioBytes {
		cleanup()
		return "", nil, Error{Message: "Compressed audio exceeds Groq's 25 MB free-plan limit.", Status: 413}
	}
	return outputPath, cleanup, nil
}
