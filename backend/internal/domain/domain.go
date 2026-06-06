package domain

import (
	"encoding/base64"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultEnglishLevel         = "b1"
	FreeWeeklyLimitSeconds      = 10 * 60
	SubscriberMaxSessionSeconds = 10 * 60
	MaxAudioUploadBytes         = 80 * 1024 * 1024
	MaxPhotoUploadBytes         = 4 * 1024 * 1024
)

var englishLevelSet = map[string]struct{}{
	"a1": {}, "a2": {}, "b1": {}, "b2": {}, "c1": {}, "c2": {},
}

var practiceTypeSet = map[string]struct{}{
	"free_talk": {}, "topic": {}, "photo_description": {},
}

var photoDataURLPattern = regexp.MustCompile(`(?i)^data:image/(png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=]+)$`)
var audioDataURLPattern = regexp.MustCompile(`(?i)^data:((?:audio|video)/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$`)
var recordingAudioFileURLPattern = regexp.MustCompile(`(?i)^/uploads/recordings/[a-z0-9/_-]+\.[a-z0-9]{2,10}$`)
var genericAudioFileURLPattern = regexp.MustCompile(`(?i)^/uploads/[a-z0-9/_-]+\.[a-z0-9]{2,10}$`)

var audioExtensionByMIME = map[string]string{
	"audio/webm":     "webm",
	"video/webm":     "webm",
	"audio/mp4":      "m4a",
	"audio/x-m4a":    "m4a",
	"video/mp4":      "m4a",
	"audio/ogg":      "ogg",
	"video/ogg":      "ogg",
	"audio/wav":      "wav",
	"audio/x-wav":    "wav",
	"audio/vnd.wave": "wav",
	"audio/mpeg":     "mp3",
}

type ParsedAudioDataURL struct {
	NormalizedDataURL string
	Base64            string
	Extension         string
}

func ParseEnglishLevel(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	_, ok := englishLevelSet[normalized]
	return normalized, ok
}

func NormalizeEnglishLevel(value string) string {
	if level, ok := ParseEnglishLevel(value); ok {
		return level
	}
	return DefaultEnglishLevel
}

func FormatEnglishLevel(level string) string {
	return strings.ToUpper(NormalizeEnglishLevel(level))
}

func EnglishLevelPromptGuidance(level string) string {
	switch NormalizeEnglishLevel(level) {
	case "a1":
		return "Use very basic vocabulary, short present-tense phrasing, and one clear idea per question."
	case "a2":
		return "Use simple everyday vocabulary, short sentences, and basic past/future forms."
	case "b2":
		return "Use richer vocabulary, nuanced scenarios, and natural connector words."
	case "c1":
		return "Use advanced vocabulary, abstract angles, and complex but natural phrasing."
	case "c2":
		return "Use near-native sophistication, idiomatic phrasing, and subtle distinctions."
	default:
		return "Use practical intermediate vocabulary and clear sentence structures."
	}
}

func NormalizePracticeType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if _, ok := practiceTypeSet[normalized]; ok {
		return normalized
	}
	return "topic"
}

func MaskEmail(value string) string {
	parts := strings.Split(strings.ToLower(value), "@")
	local, domain := "", ""
	if len(parts) > 0 {
		local = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		domain = strings.TrimSpace(parts[1])
	}
	if local == "" || domain == "" {
		return "us***@hidden"
	}
	if len(local) <= 2 {
		return string(local[0]) + "***@" + domain
	}
	return local[:2] + "***@" + domain
}

func ToNonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func FormatSeconds(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	minutes := seconds / 60
	rest := seconds % 60
	return strconv.Itoa(minutes) + ":" + leftPad2(rest)
}

func leftPad2(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

func HashString(value string) int {
	hash := int32(0)
	for _, r := range value {
		hash = hash*31 + int32(r)
	}
	if hash == math.MinInt32 {
		return math.MaxInt32
	}
	if hash < 0 {
		return int(-hash)
	}
	return int(hash)
}

func ParseTimestamp(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Now().UTC()
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now().UTC()
	}
	return parsed.UTC()
}

func NormalizeTranscript(value string) string {
	return truncate(collapseWhitespace(strings.TrimSpace(value)), 20000)
}

func NormalizePhotoObject(value string) *string {
	normalized := truncate(collapseWhitespace(strings.TrimSpace(value)), 120)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func NormalizePhotoDataURL(value string) *string {
	normalized := strings.TrimSpace(value)
	match := photoDataURLPattern.FindStringSubmatch(normalized)
	if match == nil {
		return nil
	}
	mime := strings.ToLower(match[1])
	if mime == "jpg" {
		mime = "jpeg"
	}
	payload := strings.TrimSpace(match[2])
	bytes := approximateBase64Bytes(payload)
	if bytes <= 0 || bytes > MaxPhotoUploadBytes {
		return nil
	}
	out := "data:image/" + mime + ";base64," + payload
	return &out
}

func ParseIncomingAudioDataURL(value string) *ParsedAudioDataURL {
	normalized := strings.TrimSpace(value)
	match := audioDataURLPattern.FindStringSubmatch(normalized)
	if match == nil {
		return nil
	}
	mediaType := strings.ToLower(strings.ReplaceAll(match[1], " ", ""))
	baseMIME := strings.SplitN(mediaType, ";", 2)[0]
	extension := ResolveAudioExtension(baseMIME)
	if extension == "" {
		return nil
	}
	payload := strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimSpace(match[2]))
	if !regexp.MustCompile(`^[A-Za-z0-9+/=]+$`).MatchString(payload) {
		return nil
	}
	bytes := approximateBase64Bytes(payload)
	if bytes <= 0 || bytes > MaxAudioUploadBytes {
		return nil
	}
	return &ParsedAudioDataURL{
		NormalizedDataURL: "data:" + mediaType + ";base64," + payload,
		Base64:            payload,
		Extension:         extension,
	}
}

func ResolveAudioExtension(baseMIME string) string {
	baseMIME = strings.ToLower(strings.TrimSpace(baseMIME))
	if mapped := audioExtensionByMIME[baseMIME]; mapped != "" {
		return mapped
	}
	if !strings.HasPrefix(baseMIME, "audio/") && !strings.HasPrefix(baseMIME, "video/") {
		return ""
	}
	subtype := ""
	if strings.HasPrefix(baseMIME, "audio/") {
		subtype = strings.TrimPrefix(baseMIME, "audio/")
	} else {
		subtype = strings.TrimPrefix(baseMIME, "video/")
	}
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

func NormalizeStoredRecordingAudioSource(value string) *string {
	normalized := strings.TrimSpace(value)
	if recordingAudioFileURLPattern.MatchString(normalized) {
		return &normalized
	}
	parsed := ParseIncomingAudioDataURL(normalized)
	if parsed == nil {
		return nil
	}
	return &parsed.NormalizedDataURL
}

func NormalizeStoredGenericAudioSource(value string) *string {
	normalized := strings.TrimSpace(value)
	if genericAudioFileURLPattern.MatchString(normalized) {
		return &normalized
	}
	parsed := ParseIncomingAudioDataURL(normalized)
	if parsed == nil {
		return nil
	}
	return &parsed.NormalizedDataURL
}

func SanitizePathSegment(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(normalized, "_")
	normalized = truncate(normalized, 80)
	if normalized == "" {
		return "user"
	}
	return normalized
}

func NormalizeInterests(values []string, limit int) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range values {
		value := collapseWhitespace(strings.TrimSpace(raw))
		if value == "" || len(value) > 80 {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func URLQueryAll(values url.Values, key string) []string {
	raw := values[key]
	if raw == nil {
		return []string{}
	}
	return raw
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

func DecodeBase64(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
