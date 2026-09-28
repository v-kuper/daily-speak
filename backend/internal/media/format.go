package media

import (
	"regexp"
	"strings"
)

const (
	MaxAudioUploadBytes                    = 80 * 1024 * 1024
	MaxPhotoUploadBytes                    = 4 * 1024 * 1024
	MaxInterviewTurnAudioBytes             = 24 * 1024 * 1024
	MaxAccountInterviewTurnAudioTotalBytes = 24 * 1024 * 1024
	MaxGuestInterviewTurnAudioTotalBytes   = 8 * 1024 * 1024
)

var audioExtensionByMIME = map[string]string{
	"audio/webm": "webm", "video/webm": "webm", "audio/mp4": "m4a", "audio/x-m4a": "m4a",
	"video/mp4": "m4a", "audio/ogg": "ogg", "video/ogg": "ogg", "audio/wav": "wav",
	"audio/x-wav": "wav", "audio/vnd.wave": "wav", "audio/mpeg": "mp3",
}

func NormalizePhotoObject(value string) *string {
	normalized := truncate(strings.Join(strings.Fields(strings.TrimSpace(value)), " "), 120)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func ResolveAudioExtension(baseMIME string) string {
	baseMIME = strings.ToLower(strings.TrimSpace(baseMIME))
	if mapped := audioExtensionByMIME[baseMIME]; mapped != "" {
		return mapped
	}
	if !strings.HasPrefix(baseMIME, "audio/") && !strings.HasPrefix(baseMIME, "video/") {
		return ""
	}
	subtype := strings.TrimPrefix(strings.TrimPrefix(baseMIME, "audio/"), "video/")
	subtype = strings.TrimPrefix(subtype, "x-")
	switch subtype {
	case "mpeg":
		return "mp3"
	case "mp4":
		return "m4a"
	case "wave":
		return "wav"
	}
	cleaned := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(subtype, "")
	if cleaned == "" || len(cleaned) > 10 {
		return ""
	}
	return cleaned
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
