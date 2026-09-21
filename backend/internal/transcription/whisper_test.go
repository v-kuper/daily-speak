package transcription

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAITranscriptionArgsDefaultToMultilingualMixedSpeech(t *testing.T) {
	t.Setenv("WHISPER_OPENAI_MODEL", "")
	t.Setenv("WHISPER_LANGUAGE", "")
	t.Setenv("WHISPER_INITIAL_PROMPT", "")

	args := openAITranscriptionArgs("recording.webm", "models", "output")
	joined := strings.Join(args, " ")

	if !containsArgumentPair(args, "--model", "base") {
		t.Fatalf("expected multilingual base model, got %#v", args)
	}
	if containsArgument(args, "--language") {
		t.Fatalf("expected automatic language detection instead of a forced language, got %#v", args)
	}
	if !containsArgumentPair(args, "--carry_initial_prompt", "True") {
		t.Fatalf("expected mixed-language context to be carried across long recordings, got %#v", args)
	}
	if !strings.Contains(joined, "Russian") || !strings.Contains(joined, "Cyrillic") || !strings.Contains(joined, "русские слова") {
		t.Fatalf("expected mixed English-Russian transcription context, got %#v", args)
	}
}

func TestOpenAITranscriptionArgsHonorExplicitLanguageOverride(t *testing.T) {
	t.Setenv("WHISPER_LANGUAGE", "en")

	args := openAITranscriptionArgs("recording.webm", "models", "output")

	if !containsArgumentPair(args, "--language", "en") {
		t.Fatalf("expected explicit language override, got %#v", args)
	}
}

func containsArgument(args []string, expected string) bool {
	for _, arg := range args {
		if arg == expected {
			return true
		}
	}
	return false
}

func containsArgumentPair(args []string, key string, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			return true
		}
	}
	return false
}

func TestResolveCommandEnvPreservesAbsolutePath(t *testing.T) {
	t.Setenv("WHISPER_PYTHON_BIN", "/opt/whisper/bin/python")

	got := resolveCommandEnv("WHISPER_PYTHON_BIN")

	if got != "/opt/whisper/bin/python" {
		t.Fatalf("expected absolute python path to be preserved, got %q", got)
	}
}

func TestPythonCandidatesPreferConfiguredAbsolutePythonPath(t *testing.T) {
	t.Setenv("WHISPER_PYTHON_BIN", "/opt/whisper/bin/python")

	candidates := pythonCandidates()

	if len(candidates) == 0 || candidates[0] != "/opt/whisper/bin/python" {
		t.Fatalf("expected first python candidate to be configured absolute path, got %#v", candidates)
	}
}

func TestResolvePathEnvPreservesDockerAbsolutePath(t *testing.T) {
	t.Setenv("WHISPER_OPENAI_CACHE_DIR", "/app/tools/whisper/cache")

	got := resolvePathEnv("WHISPER_OPENAI_CACHE_DIR", "")

	if got != "/app/tools/whisper/cache" {
		t.Fatalf("expected docker cache path to be preserved, got %q", got)
	}
}

func TestReadOpenAITranscriptRejectsDiagnosticOutputWhenTranscriptFileIsMissing(t *testing.T) {
	transcriptPath := filepath.Join(t.TempDir(), "missing.txt")
	diagnosticOutput := "0%| | 0.00/139M Traceback: EBML header parsing failed"

	transcript, err := readOpenAITranscript(transcriptPath, diagnosticOutput)

	if err == nil {
		t.Fatal("expected missing transcript file to be rejected")
	}
	if transcript != "" {
		t.Fatalf("expected diagnostics not to become a transcript, got %q", transcript)
	}
	if strings.Contains(err.Error(), diagnosticOutput) {
		t.Fatalf("expected a concise user-facing error, got %q", err.Error())
	}
}

func TestCppTranscriptionUsesAutomaticLanguageDetectionByDefault(t *testing.T) {
	t.Setenv("WHISPER_LANGUAGE", "")

	if got := cppLanguage(); got != "auto" {
		t.Fatalf("expected whisper.cpp automatic language detection, got %q", got)
	}
}

func TestCppTranscriptionCarriesMixedLanguageContext(t *testing.T) {
	t.Setenv("WHISPER_LANGUAGE", "")
	t.Setenv("WHISPER_INITIAL_PROMPT", "")

	args := cppTranscriptionArgs("model.bin", "recording.webm", "output")

	if !containsArgumentPair(args, "--prompt", defaultInitialPrompt) {
		t.Fatalf("expected whisper.cpp mixed-language prompt, got %#v", args)
	}
	if !containsArgument(args, "--carry-initial-prompt") {
		t.Fatalf("expected whisper.cpp to carry the prompt across the recording, got %#v", args)
	}
}

func TestCppModelCandidatesExcludeEnglishOnlyModels(t *testing.T) {
	t.Setenv("WHISPER_LANGUAGE", "auto")

	for _, candidate := range cppModelCandidates() {
		if strings.Contains(candidate, ".en.") {
			t.Fatalf("expected only multilingual whisper.cpp models, got %q", candidate)
		}
	}
}

func TestCppModelCandidatesIncludeEnglishOnlyModelsForExplicitEnglish(t *testing.T) {
	t.Setenv("WHISPER_LANGUAGE", "en")

	found := false
	for _, candidate := range cppModelCandidates() {
		if strings.Contains(candidate, ".en.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected explicit English configuration to retain .en model discovery")
	}
}
