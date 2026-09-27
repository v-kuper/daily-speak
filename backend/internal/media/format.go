package media

import (
	"encoding/base64"
	"regexp"
	"strings"
)

const (
	MaxAudioUploadBytes = 80 * 1024 * 1024
	MaxPhotoUploadBytes = 4 * 1024 * 1024
)

var photoDataURLPattern = regexp.MustCompile(`(?i)^data:image/(png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=]+)$`)
var audioDataURLPattern = regexp.MustCompile(`(?i)^data:((?:audio|video)/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$`)
var normalizedBase64Pattern = regexp.MustCompile(`^[A-Za-z0-9+/=]+$`)
var genericAudioFileURLPattern = regexp.MustCompile(`(?i)^/uploads/[a-z0-9/_-]+\.[a-z0-9]{2,10}$`)

var audioExtensionByMIME = map[string]string{
	"audio/webm": "webm", "video/webm": "webm", "audio/mp4": "m4a", "audio/x-m4a": "m4a",
	"video/mp4": "m4a", "audio/ogg": "ogg", "video/ogg": "ogg", "audio/wav": "wav",
	"audio/x-wav": "wav", "audio/vnd.wave": "wav", "audio/mpeg": "mp3",
}

type ParsedAudioDataURL struct {
	NormalizedDataURL string
	Base64            string
	Extension         string
}

func NormalizePhotoObject(value string) *string {
	normalized := truncate(strings.Join(strings.Fields(strings.TrimSpace(value)), " "), 120)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func NormalizePhotoDataURL(value string) *string {
	match := photoDataURLPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return nil
	}
	mime := strings.ToLower(match[1])
	if mime == "jpg" {
		mime = "jpeg"
	}
	payload := strings.TrimSpace(match[2])
	if bytes := approximateBase64Bytes(payload); bytes <= 0 || bytes > MaxPhotoUploadBytes {
		return nil
	}
	out := "data:image/" + mime + ";base64," + payload
	return &out
}

func ParseIncomingAudioDataURL(value string) *ParsedAudioDataURL {
	match := audioDataURLPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return nil
	}
	mediaType := strings.ToLower(strings.ReplaceAll(match[1], " ", ""))
	extension := ResolveAudioExtension(strings.SplitN(mediaType, ";", 2)[0])
	if extension == "" {
		return nil
	}
	payload := strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimSpace(match[2]))
	if !normalizedBase64Pattern.MatchString(payload) {
		return nil
	}
	if bytes := approximateBase64Bytes(payload); bytes <= 0 || bytes > MaxAudioUploadBytes {
		return nil
	}
	return &ParsedAudioDataURL{NormalizedDataURL: "data:" + mediaType + ";base64," + payload, Base64: payload, Extension: extension}
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

func NormalizeStoredGenericAudioSource(value string) *string {
	return normalizeStoredAudio(value, genericAudioFileURLPattern)
}

func DecodeBase64(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}

func normalizeStoredAudio(value string, filePattern *regexp.Regexp) *string {
	normalized := strings.TrimSpace(value)
	if filePattern.MatchString(normalized) {
		return &normalized
	}
	parsed := ParseIncomingAudioDataURL(normalized)
	if parsed == nil {
		return nil
	}
	return &parsed.NormalizedDataURL
}

func approximateBase64Bytes(value string) int {
	padding := 0
	if strings.HasSuffix(value, "==") {
		padding = 2
	} else if strings.HasSuffix(value, "=") {
		padding = 1
	}
	return len(value)*3/4 - padding
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
