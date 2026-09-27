package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func ProbeAudioDuration(ctx context.Context, audioPath string) (time.Duration, error) {
	binary, err := resolveFFprobeBinary()
	if err != nil {
		return 0, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, binary,
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", audioPath,
	).CombinedOutput()
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
		return 0, errors.New("ffprobe timed out")
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return 0, fmt.Errorf("ffprobe failed: %s", truncateProbeMessage(message, 300))
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil || seconds <= 0 || seconds > 24*60*60 {
		return 0, errors.New("ffprobe returned an invalid duration")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func truncateProbeMessage(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func resolveFFprobeBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("FFPROBE_BINARY_PATH")); configured != "" {
		if path, err := exec.LookPath(configured); err == nil {
			return path, nil
		}
		return "", errors.New("FFPROBE_BINARY_PATH is not executable")
	}
	name := "ffprobe"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if ffmpeg := strings.TrimSpace(os.Getenv("WHISPER_FFMPEG_BIN")); ffmpeg != "" {
		candidate := filepath.Join(filepath.Dir(ffmpeg), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	return "", errors.New("ffprobe is required to verify guest audio duration")
}
