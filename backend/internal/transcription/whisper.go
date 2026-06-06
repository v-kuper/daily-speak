package transcription

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWhisperRoot      = "tools/whisper"
	defaultWhisperTimeoutMS = 180000
	maxTranscriptLength     = 20000
	defaultOpenAIModel      = "base.en"
)

type Error struct {
	Message string
	Status  int
}

func (e Error) Error() string {
	return e.Message
}

func TranscribeAudioWithLocalWhisper(ctx context.Context, audioFilePath string) (string, error) {
	normalized := strings.TrimSpace(audioFilePath)
	if normalized == "" {
		return "", Error{Message: "Audio file path is required for transcription.", Status: 500}
	}
	switch resolveBackend() {
	case "cpp":
		return transcribeWithCpp(ctx, normalized)
	case "openai":
		return transcribeWithOpenAI(ctx, normalized)
	default:
		transcript, cppErr := transcribeWithCpp(ctx, normalized)
		if cppErr == nil {
			return transcript, nil
		}
		transcript, openAIErr := transcribeWithOpenAI(ctx, normalized)
		if openAIErr == nil {
			return transcript, nil
		}
		status := 502
		var typed Error
		if errors.As(openAIErr, &typed) {
			status = typed.Status
		}
		return "", Error{Message: "Whisper backends failed. cpp: " + cppErr.Error() + " | openai: " + openAIErr.Error(), Status: status}
	}
}

func transcribeWithCpp(ctx context.Context, audioFilePath string) (string, error) {
	binaryPath, err := resolveCppBinaryPath()
	if err != nil {
		return "", err
	}
	modelPath, err := resolveCppModelPath()
	if err != nil {
		return "", err
	}
	tempDir, err := os.MkdirTemp("", "daily-whisper-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	outputPrefix := filepath.Join(tempDir, "transcript")
	args := []string{"-m", modelPath, "-f", audioFilePath, "-l", normalizeLanguage(os.Getenv("WHISPER_LANGUAGE")), "-t", strconv.Itoa(resolveThreads()), "-otxt", "-of", outputPrefix}
	output, err := runCommand(ctx, binaryPath, args, nil)
	if err != nil {
		return "", err
	}
	transcriptBytes, _ := os.ReadFile(outputPrefix + ".txt")
	transcript := normalizeTranscript(string(transcriptBytes))
	if transcript == "" {
		transcript = normalizeTranscript(output)
	}
	if transcript == "" {
		return "", Error{Message: "Whisper returned an empty transcript. Check audio format or model configuration.", Status: 422}
	}
	return transcript, nil
}

func transcribeWithOpenAI(ctx context.Context, audioFilePath string) (string, error) {
	tempDir, err := os.MkdirTemp("", "daily-whisper-openai-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	modelDir := resolvePathEnv("WHISPER_OPENAI_MODEL_DIR", filepath.Join(defaultWhisperRoot, "openai-models"))
	_ = os.MkdirAll(modelDir, 0o755)
	cacheDir := resolvePathEnv("WHISPER_OPENAI_CACHE_DIR", filepath.Join(defaultWhisperRoot, "cache"))
	_ = os.MkdirAll(cacheDir, 0o755)

	args := []string{
		"-m", "whisper",
		audioFilePath,
		"--task", "transcribe",
		"--model", envDefault("WHISPER_OPENAI_MODEL", defaultOpenAIModel),
		"--model_dir", modelDir,
		"--language", normalizeLanguage(os.Getenv("WHISPER_LANGUAGE")),
		"--threads", strconv.Itoa(resolveThreads()),
		"--output_dir", tempDir,
		"--output_format", "txt",
		"--verbose", "False",
		"--fp16", resolveOpenAIFp16(),
	}
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
		output, err := runCommand(ctx, command, args, env)
		if err != nil {
			lastErr = err
			message := err.Error()
			if isFfmpegMissing(message) {
				return "", Error{Message: "OpenAI Whisper requires ffmpeg. Put binary at " + ffmpegPath + " or install ffmpeg globally, then restart dev server.", Status: 500}
			}
			if isCommandNotFound(message) || isWhisperModuleMissing(message) {
				continue
			}
			continue
		}
		transcriptPath := filepath.Join(tempDir, strings.TrimSuffix(filepath.Base(audioFilePath), filepath.Ext(audioFilePath))+".txt")
		transcriptBytes, _ := os.ReadFile(transcriptPath)
		transcript := normalizeTranscript(string(transcriptBytes))
		if transcript == "" {
			transcript = normalizeTranscript(output)
		}
		if transcript == "" {
			return "", Error{Message: "OpenAI Whisper returned an empty transcript. Check microphone audio quality and model settings.", Status: 422}
		}
		return transcript, nil
	}
	if lastErr != nil {
		message := lastErr.Error()
		if isWhisperModuleMissing(message) {
			return "", Error{Message: "Python module whisper is missing. Install it with: pip install -U openai-whisper", Status: 500}
		}
		if isCommandNotFound(message) {
			return "", Error{Message: "Python is not available for OpenAI Whisper. Set WHISPER_PYTHON_BIN or install python3.", Status: 500}
		}
		return "", lastErr
	}
	return "", Error{Message: "OpenAI Whisper backend is not configured. Set WHISPER_PYTHON_BIN and install openai-whisper.", Status: 500}
}

func runCommand(ctx context.Context, command string, args []string, env []string) (string, error) {
	timeout := time.Duration(resolveTimeoutMS()) * time.Millisecond
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, command, args...)
	if env != nil {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	if commandCtx.Err() == context.DeadlineExceeded {
		return "", Error{Message: "Whisper command timed out after " + strconv.Itoa(resolveTimeoutMS()) + "ms.", Status: 504}
	}
	if err != nil {
		details := strings.TrimSpace(string(output))
		if details == "" {
			details = err.Error()
		}
		return "", Error{Message: "Whisper transcription failed: " + truncate(details, 500), Status: 502}
	}
	return string(output), nil
}

func resolveBackend() string {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("WHISPER_BACKEND")))
	switch raw {
	case "cpp", "whisper.cpp", "whispercpp":
		return "cpp"
	case "openai", "python", "whisper":
		return "openai"
	default:
		return "auto"
	}
}

func resolveTimeoutMS() int {
	parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv("WHISPER_TIMEOUT_MS")))
	if err != nil || parsed <= 0 {
		return defaultWhisperTimeoutMS
	}
	if parsed > 900000 {
		return 900000
	}
	return parsed
}

func resolveThreads() int {
	parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv("WHISPER_THREADS")))
	if err == nil && parsed > 0 {
		if parsed > 16 {
			return 16
		}
		return parsed
	}
	available := runtime.NumCPU()
	if available > 8 {
		return 8
	}
	if available < 1 {
		return 1
	}
	return available
}

func resolveCppBinaryPath() (string, error) {
	if envPath := resolvePathEnv("WHISPER_BINARY_PATH", ""); envPath != "" {
		if executable(envPath) {
			return envPath, nil
		}
		return "", Error{Message: "WHISPER_BINARY_PATH is set but file is not executable: " + envPath + ".", Status: 500}
	}
	for _, candidate := range []string{
		filepath.Join(defaultWhisperRoot, "bin", "whisper-cli"),
		filepath.Join(defaultWhisperRoot, "bin", "main"),
		filepath.Join("tools", "whisper.cpp", "build", "bin", "whisper-cli"),
		filepath.Join("tools", "whisper.cpp", "main"),
	} {
		if executable(candidate) {
			return candidate, nil
		}
	}
	return "", Error{Message: "Local Whisper binary not found. Place whisper-cli in tools/whisper/bin or set WHISPER_BINARY_PATH.", Status: 500}
}

func resolveCppModelPath() (string, error) {
	if envPath := resolvePathEnv("WHISPER_MODEL_PATH", ""); envPath != "" {
		if exists(envPath) {
			return envPath, nil
		}
		return "", Error{Message: "WHISPER_MODEL_PATH is set but file is missing: " + envPath + ".", Status: 500}
	}
	for _, candidate := range []string{
		filepath.Join(defaultWhisperRoot, "models", "ggml-base.en.bin"),
		filepath.Join(defaultWhisperRoot, "models", "ggml-base.bin"),
		filepath.Join(defaultWhisperRoot, "models", "ggml-small.en.bin"),
		filepath.Join("tools", "whisper.cpp", "models", "ggml-base.en.bin"),
		filepath.Join("tools", "whisper.cpp", "models", "ggml-base.bin"),
	} {
		if exists(candidate) {
			return candidate, nil
		}
	}
	return "", Error{Message: "Whisper model not found. Place ggml model in tools/whisper/models or set WHISPER_MODEL_PATH.", Status: 500}
}

func pythonCandidates() []string {
	candidates := []string{}
	if envPython := resolveCommandEnv("WHISPER_PYTHON_BIN"); envPython != "" {
		candidates = append(candidates, envPython)
	}
	candidates = append(candidates, filepath.Join(".venv", "bin", "python"), filepath.Join("venv", "bin", "python"), "python3", "python")
	return dedupe(candidates)
}

func normalizeLanguage(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if regexp.MustCompile(`^[a-z]{2,12}$`).MatchString(normalized) {
		return normalized
	}
	return "en"
}

func resolveOpenAIFp16() string {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("WHISPER_OPENAI_FP16")))
	if raw == "1" || raw == "true" {
		return "True"
	}
	return "False"
}

func resolveOpenAIDevice() string {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("WHISPER_OPENAI_DEVICE")))
	if regexp.MustCompile(`^[a-z0-9:_-]{2,32}$`).MatchString(raw) {
		return raw
	}
	return ""
}

func resolvePathEnv(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = fallback
	}
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(".", value)
}

func resolveCommandEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, ".") || strings.ContainsAny(value, `/\`) {
		return filepath.Join(".", value)
	}
	return value
}

func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func normalizeTranscript(raw string) string {
	out := regexp.MustCompile(`\[[0-9:.]+\s*-->\s*[0-9:.]+\]`).ReplaceAllString(raw, " ")
	out = strings.Join(strings.Fields(out), " ")
	return truncate(out, maxTranscriptLength)
}

func isCommandNotFound(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "enoent") || strings.Contains(lower, "not found")
}

func isWhisperModuleMissing(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "no module named whisper") || strings.Contains(lower, "modulenotfounderror")
}

func isFfmpegMissing(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "ffmpeg") && (strings.Contains(lower, "not found") || strings.Contains(lower, "no such file") || strings.Contains(lower, "required") || strings.Contains(lower, "install"))
}

func envDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	if len(value) > 80 {
		return value[:80]
	}
	return value
}

func dedupe(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		if value == "" {
			continue
		}
		key := value
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
