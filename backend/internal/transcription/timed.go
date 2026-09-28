package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// TimedSegment is a piece of the final, full-file transcription. Text keeps
// Whisper's original spacing so adjacent pieces can be joined without adding
// or losing words around punctuation.
type TimedSegment struct {
	StartMS int
	EndMS   int
	Text    string
}

type TimedResult struct {
	Text     string
	Segments []TimedSegment
}

// TranscribeTimedAudioWithLocalWhisper is used for final interview audio.
// The normal transcription path remains available for recordings without a
// question timeline and as a fallback if timed output is unavailable.
func TranscribeTimedAudioWithLocalWhisper(ctx context.Context, audioFilePath string) (TimedResult, error) {
	normalized := strings.TrimSpace(audioFilePath)
	if normalized == "" {
		return TimedResult{}, Error{Message: "Audio file path is required for transcription.", Status: 500}
	}
	switch resolveBackend() {
	case "cpp":
		return transcribeTimedWithCpp(ctx, normalized)
	case "openai":
		return transcribeTimedWithOpenAI(ctx, normalized)
	default:
		result, cppErr := transcribeTimedWithCpp(ctx, normalized)
		if cppErr == nil {
			return result, nil
		}
		result, openAIErr := transcribeTimedWithOpenAI(ctx, normalized)
		if openAIErr == nil {
			return result, nil
		}
		return TimedResult{}, errors.Join(cppErr, openAIErr)
	}
}

func transcribeTimedWithCpp(ctx context.Context, audioFilePath string) (TimedResult, error) {
	binaryPath, err := resolveCppBinaryPath()
	if err != nil {
		return TimedResult{}, err
	}
	modelPath, err := resolveCppModelPath()
	if err != nil {
		return TimedResult{}, err
	}
	tempDir, err := os.MkdirTemp("", "daily-whisper-timed-")
	if err != nil {
		return TimedResult{}, err
	}
	defer os.RemoveAll(tempDir)
	outputPrefix := filepath.Join(tempDir, "transcript")
	args := cppTranscriptionArgs(modelPath, audioFilePath, outputPrefix)
	for index, arg := range args {
		if arg == "-otxt" {
			args[index] = "-oj"
			break
		}
	}
	if _, err := runCommand(ctx, binaryPath, args, nil); err != nil {
		return TimedResult{}, err
	}
	data, err := os.ReadFile(outputPrefix + ".json")
	if err != nil {
		return TimedResult{}, Error{Message: "Whisper did not produce timed JSON output.", Status: 502}
	}
	return parseCppTimedJSON(data)
}

func transcribeTimedWithOpenAI(ctx context.Context, audioFilePath string) (TimedResult, error) {
	tempDir, err := os.MkdirTemp("", "daily-whisper-openai-timed-")
	if err != nil {
		return TimedResult{}, err
	}
	defer os.RemoveAll(tempDir)
	modelDir := resolvePathEnv("WHISPER_OPENAI_MODEL_DIR", filepath.Join(defaultWhisperRoot, "openai-models"))
	_ = os.MkdirAll(modelDir, 0o755)
	cacheDir := resolvePathEnv("WHISPER_OPENAI_CACHE_DIR", filepath.Join(defaultWhisperRoot, "cache"))
	_ = os.MkdirAll(cacheDir, 0o755)
	args := openAITranscriptionArgs(audioFilePath, modelDir, tempDir)
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "--output_format" {
			args[index+1] = "json"
			break
		}
	}
	args = append(args, "--word_timestamps", "True")
	if device := resolveOpenAIDevice(); device != "" {
		args = append(args, "--device", device)
	}
	env := os.Environ()
	env = append(env, "PYTHONUTF8=1", "XDG_CACHE_HOME="+cacheDir, "TRANSFORMERS_CACHE="+filepath.Join(cacheDir, "transformers"), "HF_HOME="+filepath.Join(cacheDir, "hf"))
	ffmpegPath := resolvePathEnv("WHISPER_FFMPEG_BIN", filepath.Join("tools", "ffmpeg", "bin", "ffmpeg"))
	if executable(ffmpegPath) {
		env = append(env, "PATH="+filepath.Dir(ffmpegPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	var lastErr error
	for _, command := range pythonCandidates() {
		if _, err := runCommand(ctx, command, args, env); err != nil {
			lastErr = err
			if isFfmpegMissing(err.Error()) {
				return TimedResult{}, Error{Message: "OpenAI Whisper requires ffmpeg.", Status: 500}
			}
			continue
		}
		path := filepath.Join(tempDir, strings.TrimSuffix(filepath.Base(audioFilePath), filepath.Ext(audioFilePath))+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			return TimedResult{}, Error{Message: "OpenAI Whisper did not produce timed JSON output.", Status: 502}
		}
		return parseOpenAITimedJSON(data)
	}
	if lastErr != nil {
		return TimedResult{}, lastErr
	}
	return TimedResult{}, Error{Message: "OpenAI Whisper backend is not configured.", Status: 500}
}

func parseCppTimedJSON(data []byte) (TimedResult, error) {
	var raw struct {
		Transcription []struct {
			Offsets *struct {
				From int `json:"from"`
				To   int `json:"to"`
			} `json:"offsets"`
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return TimedResult{}, err
	}
	segments := make([]TimedSegment, 0, len(raw.Transcription))
	var original strings.Builder
	validTiming := true
	for _, item := range raw.Transcription {
		original.WriteString(item.Text)
		if item.Offsets == nil || item.Offsets.From < 0 || item.Offsets.To < item.Offsets.From {
			validTiming = false
			continue
		}
		segments = append(segments, TimedSegment{StartMS: item.Offsets.From, EndMS: item.Offsets.To, Text: item.Text})
	}
	text := normalizeTranscript(original.String())
	if text == "" {
		return TimedResult{}, Error{Message: "Whisper returned an empty transcript.", Status: 422}
	}
	if !validTiming || len([]rune(strings.Join(strings.Fields(original.String()), " "))) > maxTranscriptLength {
		segments = nil
	}
	return TimedResult{Text: text, Segments: segments}, nil
}

func parseOpenAITimedJSON(data []byte) (TimedResult, error) {
	var raw struct {
		Text     string `json:"text"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
			Words []struct {
				Start float64 `json:"start"`
				End   float64 `json:"end"`
				Word  string  `json:"word"`
			} `json:"words"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return TimedResult{}, err
	}
	text := normalizeTranscript(raw.Text)
	if text == "" {
		return TimedResult{}, Error{Message: "Whisper returned an empty transcript.", Status: 422}
	}
	parts := make([]TimedSegment, 0, len(raw.Segments))
	var joined strings.Builder
	validTiming := true
	for _, segment := range raw.Segments {
		joined.WriteString(segment.Text)
		segmentParts := []TimedSegment{{StartMS: milliseconds(segment.Start), EndMS: milliseconds(segment.End), Text: segment.Text}}
		if len(segment.Words) > 0 {
			var wordsText strings.Builder
			words := make([]TimedSegment, 0, len(segment.Words))
			for _, word := range segment.Words {
				wordsText.WriteString(word.Word)
				words = append(words, TimedSegment{StartMS: milliseconds(word.Start), EndMS: milliseconds(word.End), Text: word.Word})
			}
			if normalizeTranscript(wordsText.String()) == normalizeTranscript(segment.Text) {
				segmentParts = words
			}
		}
		for _, part := range segmentParts {
			if part.StartMS < 0 || part.EndMS < part.StartMS {
				validTiming = false
			}
			parts = append(parts, part)
		}
	}
	if normalizeTranscript(joined.String()) != text || len([]rune(strings.Join(strings.Fields(raw.Text), " "))) > maxTranscriptLength {
		validTiming = false
	}
	if !validTiming {
		parts = nil
	}
	return TimedResult{Text: text, Segments: parts}, nil
}

func milliseconds(seconds float64) int {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > float64(math.MaxInt/1000) {
		return -1
	}
	return int(math.Round(seconds * 1000))
}
