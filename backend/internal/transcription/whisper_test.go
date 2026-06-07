package transcription

import "testing"

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
