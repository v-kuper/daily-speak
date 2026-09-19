package transcription

import (
	"path/filepath"
	"strings"
	"testing"
)

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
