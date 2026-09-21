package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/quota"
	"github.com/google/uuid"
)

var cyrillicPhrasePattern = regexp.MustCompile(`[\p{Cyrillic}]+(?:[- \t]+[\p{Cyrillic}]+)*`)

func (s *Server) handleCreateRecording(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	logger := logging.ForRequest("api.user.recordings.post", r)
	user, ok := s.authorizedUser(w, r, "api.user.recordings.post")
	if !ok {
		return
	}

	var payload struct {
		Recording map[string]any `json:"recording"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	source := payload.Recording
	practiceType := domain.NormalizePracticeType(stringAny(source["practiceType"]))
	topic := strings.TrimSpace(stringAny(source["topic"]))
	duration := parseIntAny(source["duration"])
	rawAudio := stringAny(source["audioDataUrl"])
	parsedAudio := domain.ParseIncomingAudioDataURL(rawAudio)
	rawPhoto := stringAny(source["photoDataUrl"])
	photoDataURL := domain.NormalizePhotoDataURL(rawPhoto)
	photoObject := domain.NormalizePhotoObject(stringAny(source["photoObject"]))

	if rawAudio != "" && parsedAudio == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Audio must be a valid recording under 80MB."})
		return
	}
	if parsedAudio == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Audio recording is required."})
		return
	}
	if rawPhoto != "" && photoDataURL == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Photo must be a valid image under 4MB."})
		return
	}
	if practiceType == "photo_description" {
		if photoDataURL == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Photo is required for photo description practice."})
			return
		}
		if topic == "" {
			topic = "Photo description"
		}
	}
	if topic == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording topic is required."})
		return
	}
	if duration < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording duration is invalid."})
		return
	}

	qBefore, err := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &user.IsSubscriber)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	if quotaError := recordingQuotaError(qBefore, duration); quotaError != nil {
		writeJSON(w, quotaError.status, map[string]string{"error": quotaError.message})
		return
	}

	recordingID := uuid.NewString()
	savedAudio, err := saveAudioFile("recordings", user.ID, recordingID, parsedAudio)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	cleanupAudio := savedAudio.absolutePath
	defer func() {
		if cleanupAudio != "" {
			_ = os.Remove(cleanupAudio)
		}
	}()

	timestamp := domain.ParseTimestamp(stringAny(source["timestamp"]))

	var inserted struct {
		ID                  string
		Topic               string
		Duration            int
		Timestamp           time.Time
		Status              string
		Transcript          string
		CorrectedTranscript string
		Suggestions         []byte
		ProcessingStage     *string
		PracticeType        string
		AudioDataURL        *string
		PhotoDataURL        *string
		PhotoObject         *string
		ProcessingError     *string
		ShadowingStatus     string
		ShadowingAudioURL   *string
		ShadowingError      *string
		ShadowingUpdatedAt  time.Time
	}
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, corrected_transcript, suggestions, practice_type, audio_data_url, photo_data_url, photo_object, status, processing_stage)
		VALUES
		  ($1, $2, $3, $4, $5, '', '', '[]'::jsonb, $6, $7, $8, $9, 'processing', 'transcribing')
		RETURNING id, topic, duration, timestamp, status, transcript, corrected_transcript, suggestions, processing_stage, practice_type, audio_data_url, photo_data_url, photo_object, processing_error,
		          shadowing_status, shadowing_audio_url, shadowing_error, shadowing_updated_at`,
		recordingID,
		user.ID,
		truncateRunes(topic, 300),
		duration,
		timestamp,
		practiceType,
		savedAudio.publicURL,
		stringOrNil(practiceType == "photo_description", photoDataURL),
		stringOrNil(practiceType == "photo_description", photoObject),
	).Scan(&inserted.ID, &inserted.Topic, &inserted.Duration, &inserted.Timestamp, &inserted.Status, &inserted.Transcript, &inserted.CorrectedTranscript, &inserted.Suggestions, &inserted.ProcessingStage, &inserted.PracticeType, &inserted.AudioDataURL, &inserted.PhotoDataURL, &inserted.PhotoObject, &inserted.ProcessingError, &inserted.ShadowingStatus, &inserted.ShadowingAudioURL, &inserted.ShadowingError, &inserted.ShadowingUpdatedAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	cleanupAudio = ""
	s.processRecordingInBackground(recordingID, user.ID, savedAudio.absolutePath, topic, practiceType, photoObject, user.EnglishLevel)

	q := recordingQuotaAfterSave(qBefore, duration)
	if refreshedQuota, quotaErr := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &user.IsSubscriber); quotaErr == nil {
		q = refreshedQuota
	} else {
		logger.Warn("recording.quota_refresh_failed", logging.ErrorMeta(quotaErr))
	}
	recording := recordingResponse{
		ID:                  inserted.ID,
		Topic:               inserted.Topic,
		Duration:            domain.ToNonNegativeInt(inserted.Duration),
		Timestamp:           inserted.Timestamp.UTC().Format(time.RFC3339Nano),
		Status:              normalizeRecordingStatus(inserted.Status),
		Transcript:          inserted.Transcript,
		CorrectedTranscript: inserted.CorrectedTranscript,
		Suggestions:         normalizeSuggestions(inserted.Suggestions, 0),
		ProcessingStage:     normalizeRecordingProcessingStage(inserted.ProcessingStage),
		PracticeType:        domain.NormalizePracticeType(inserted.PracticeType),
		AudioDataURL:        normalizeOptionalAudio(inserted.AudioDataURL, true),
		PhotoDataURL:        normalizeOptionalPhoto(inserted.PhotoDataURL),
		PhotoObject:         normalizeOptionalPhotoObject(inserted.PhotoObject),
		ProcessingError:     normalizeOptionalProcessingError(inserted.ProcessingError),
		ShadowingStatus:     normalizeShadowingStatus(inserted.ShadowingStatus),
		ShadowingAudioURL:   normalizeOptionalShadowingAudio(inserted.ShadowingAudioURL),
		ShadowingError:      normalizeOptionalProcessingError(inserted.ShadowingError),
		ShadowingUpdatedAt:  inserted.ShadowingUpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	logger.Info("request.success", map[string]any{"status": 201, "durationMs": logging.ElapsedMs(started), "userId": user.ID, "recordingId": recording.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"recording": recording, "quota": q})
}

func marshalSuggestions(suggestions []suggestion) string {
	suggestionJSON, _ := json.Marshal(suggestions)
	return string(suggestionJSON)
}

func (s *Server) generateRecordingSuggestions(ctx context.Context, transcript string, topic string, interests []string, practiceType string, photoObject *string, englishLevel string, logger logging.Logger) ([]suggestion, error) {
	if strings.TrimSpace(transcript) == "" {
		return []suggestion{}, nil
	}
	settings := ai.ResolveSettingsForUser()
	useJSONFormat := !settings.IsThinkingModel
	seed := absMod(domain.HashString(strings.ToLower(topic))*131+domain.HashString(transcript)*17, 2147483647)
	prompt := recordingSuggestionsPrompt(transcript, topic, interests, practiceType, photoObject, englishLevel)
	requiredRussianPhrases := extractRussianPhrases(recordingTranscriptForPrompt(transcript))
	for attempt := 0; attempt < 2; attempt++ {
		strictJSON := attempt > 0
		body := map[string]any{
			"model":  settings.Model,
			"stream": false,
			"think":  ai.ThinkOption(settings.IsThinkingModel),
			"messages": []map[string]string{
				{"role": "system", "content": chooseString(strictJSON, "Return strict valid JSON only. No markdown. No prose.", "You analyze learner transcripts and output only grammar correction JSON.")},
				{"role": "user", "content": prompt},
			},
			"options": map[string]any{
				"temperature": chooseFloat(strictJSON, 0.1, 0.3),
				"seed":        seed + attempt*97,
			},
		}
		if useJSONFormat {
			body["format"] = "json"
		}
		payload, err := s.aiClient.PostChat(ctx, body)
		if err != nil {
			logger.Warn("ollama.suggestions_request_failed", logging.ErrorMeta(err))
			return nil, errors.New("AI suggestions could not be generated. Please try again later.")
		}
		suggestions, valid := parseSuggestionsResultFromContent(ai.ExtractMessageContent(payload))
		if valid && russianSuggestionsCovered(suggestions, requiredRussianPhrases) {
			return selectRecordingSuggestions(suggestions, requiredRussianPhrases), nil
		}
	}
	return nil, errors.New("AI suggestions could not be generated. Please try again later.")
}

func (s *Server) generateNaturalTranscript(ctx context.Context, transcript string, suggestions []suggestion, englishLevel string, logger logging.Logger) (string, error) {
	if strings.TrimSpace(transcript) == "" {
		return "", errors.New("The natural English version could not be generated. Please try again later.")
	}
	settings := ai.ResolveSettingsForUser()
	useJSONFormat := !settings.IsThinkingModel
	seed := absMod(domain.HashString(transcript)*193+domain.HashString(englishLevel)*29, 2147483647)
	prompt := recordingNaturalVersionPrompt(transcript, suggestions, englishLevel)
	for attempt := 0; attempt < 2; attempt++ {
		strictJSON := attempt > 0
		body := map[string]any{
			"model":  settings.Model,
			"stream": false,
			"think":  ai.ThinkOption(settings.IsThinkingModel),
			"messages": []map[string]string{
				{"role": "system", "content": chooseString(strictJSON, "Return strict valid JSON only. No markdown. No prose.", "You rewrite learner speech as natural conversational English and output JSON only.")},
				{"role": "user", "content": prompt},
			},
			"options": map[string]any{
				"temperature": chooseFloat(strictJSON, 0.15, 0.35),
				"seed":        seed + attempt*97,
			},
		}
		if useJSONFormat {
			body["format"] = "json"
		}
		payload, err := s.aiClient.PostChat(ctx, body)
		if err != nil {
			logger.Warn("ollama.natural_transcript_request_failed", logging.ErrorMeta(err))
			return "", errors.New("The natural English version could not be generated. Please try again later.")
		}
		if correctedTranscript := parseNaturalTranscriptFromContent(ai.ExtractMessageContent(payload)); correctedTranscript != "" {
			return correctedTranscript, nil
		}
	}
	return "", errors.New("The natural English version could not be generated. Please try again later.")
}

func parseSuggestionsFromContent(content string) []suggestion {
	suggestions, _ := parseSuggestionsResultFromContent(content)
	return suggestions
}

func parseSuggestionsResultFromContent(content string) ([]suggestion, bool) {
	for _, candidate := range ai.ExtractJSONCandidates(content) {
		var payload map[string]json.RawMessage
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			continue
		}
		for _, key := range []string{"suggestions", "corrections", "mistakes", "errorAnalysis"} {
			if raw, ok := payload[key]; ok {
				var items []json.RawMessage
				if json.Unmarshal(raw, &items) != nil {
					continue
				}
				suggestions := normalizeSuggestions(raw, 0)
				if len(items) == 0 || len(suggestions) > 0 {
					return suggestions, true
				}
			}
		}
	}
	return []suggestion{}, false
}

func parseNaturalTranscriptFromContent(content string) string {
	for _, candidate := range ai.ExtractJSONCandidates(content) {
		var payload map[string]json.RawMessage
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			continue
		}
		for _, key := range []string{"correctedTranscript", "naturalTranscript", "improvedTranscript"} {
			raw, ok := payload[key]
			if !ok {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) == nil {
				if normalized := domain.NormalizeTranscript(value); normalized != "" && !containsCyrillic(normalized) {
					return normalized
				}
			}
		}
	}
	return ""
}

func extractRussianPhrases(transcript string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, phrase := range cyrillicPhrasePattern.FindAllString(transcript, -1) {
		if _, exists := seen[phrase]; exists {
			continue
		}
		seen[phrase] = struct{}{}
		out = append(out, phrase)
	}
	return out
}

func russianSuggestionsCovered(suggestions []suggestion, required []string) bool {
	for _, phrase := range required {
		covered := false
		for _, item := range suggestions {
			if validRussianSuggestion(item, phrase) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func selectRecordingSuggestions(suggestions []suggestion, required []string) []suggestion {
	out := make([]suggestion, 0, len(suggestions))
	used := map[string]struct{}{}
	for _, phrase := range required {
		for _, item := range suggestions {
			if validRussianSuggestion(item, phrase) {
				out = append(out, item)
				used[item.Wrong] = struct{}{}
				break
			}
		}
	}
	for _, item := range suggestions {
		if containsCyrillic(item.Wrong) {
			continue
		}
		if _, exists := used[item.Wrong]; exists {
			continue
		}
		out = append(out, item)
		used[item.Wrong] = struct{}{}
	}
	return out
}

func validRussianSuggestion(item suggestion, phrase string) bool {
	return item.Wrong == phrase && item.Right != "" && !containsCyrillic(item.Right)
}

func containsCyrillic(value string) bool {
	return cyrillicPhrasePattern.MatchString(value)
}

func recordingTranscriptForPrompt(transcript string) string {
	return domain.NormalizeTranscript(transcript)
}

func recordingSuggestionsPrompt(transcript string, topic string, interests []string, practiceType string, photoObject *string, englishLevel string) string {
	transcriptForPrompt := recordingTranscriptForPrompt(transcript)
	russianPhrases, _ := json.Marshal(extractRussianPhrases(transcriptForPrompt))
	parts := []string{
		`Topic: "` + topic + `".`,
		"Learner level: " + domain.FormatEnglishLevel(englishLevel) + ".",
		"Language difficulty: " + domain.EnglishLevelPromptGuidance(englishLevel),
		"You receive an English learner transcript that may contain occasional Russian words or phrases written in Cyrillic.",
		`Treat every Russian word or phrase as a word-choice mistake: put the exact Russian text in "wrong", its natural English translation in "right", and briefly explain that the learner switched to Russian.`,
		"List Russian-language corrections before ordinary grammar or word-choice corrections.",
		"Find every clear grammar or word-choice mistake in the full transcript; do not stop after an arbitrary number of corrections.",
		"Required Russian phrases: " + string(russianPhrases) + ". Include one correction for every listed phrase.",
		`Return only JSON with this exact shape: {"suggestions":[{"wrong":"...","right":"...","explanation":"..."}]}.`,
		"Do not invent mistakes that are not present in the transcript.",
		"No markdown and no extra keys.",
		`Transcript: """` + transcriptForPrompt + `""".`,
	}
	if practiceType == "photo_description" {
		parts = append(parts, "Practice mode: photo description.")
		if photoObject != nil {
			parts = append(parts, `Main photo object: "`+*photoObject+`".`)
		}
	} else if practiceType == "free_talk" {
		parts = append(parts, "Practice mode: free talk.")
	}
	if len(interests) > 0 {
		parts = append(parts, "Learner interests context: "+strings.Join(interests, ", ")+".")
	}
	return strings.Join(parts, " ")
}

func recordingNaturalVersionPrompt(transcript string, suggestions []suggestion, englishLevel string) string {
	transcriptForPrompt := recordingTranscriptForPrompt(transcript)
	suggestionsJSON, _ := json.Marshal(suggestions)
	parts := []string{
		"Learner level: " + domain.FormatEnglishLevel(englishLevel) + ".",
		recordingNaturalVersionLevelGuidance(englishLevel),
		"Rewrite the transcript as natural conversational English while you preserve the speaker's meaning, intent, and factual details.",
		"Replace every Russian word or phrase with its supplied English correction so the result is English-only.",
		"Apply the supplied corrections, fix sentence structure and word order, and remove accidental repetitions or filler that make the thought unclear.",
		"Do not invent new details, opinions, or events. Keep the result achievable and useful for a learner at the stated level.",
		`Return only JSON with this exact shape: {"correctedTranscript":"..."}.`,
		"No markdown and no extra keys.",
		"Corrections: " + string(suggestionsJSON) + ".",
		`Transcript: """` + transcriptForPrompt + `""".`,
	}
	return strings.Join(parts, " ")
}

func recordingNaturalVersionLevelGuidance(englishLevel string) string {
	switch domain.NormalizeEnglishLevel(englishLevel) {
	case "a1":
		return "Use very simple everyday vocabulary and short spoken sentences."
	case "a2":
		return "Use simple everyday vocabulary and clear spoken sentences."
	case "b2":
		return "Use natural upper-intermediate vocabulary, connectors, and varied spoken sentences."
	case "c1":
		return "Use fluent advanced vocabulary and idiomatic but precise conversational phrasing."
	case "c2":
		return "Use sophisticated near-native vocabulary, nuance, and idiomatic conversational phrasing."
	default:
		return "Use clear intermediate vocabulary and natural spoken sentence structures."
	}
}

type savedAudioFile struct {
	publicURL    string
	absolutePath string
}

func saveAudioFile(kind string, userID string, id string, audio *domain.ParsedAudioDataURL) (savedAudioFile, error) {
	userDir := domain.SanitizePathSegment(userID)
	fileName := id + "." + audio.Extension
	publicBase := "/uploads/" + kind
	storageRoot := filepath.Join(resolveUploadsDir(), kind)
	directory := filepath.Join(storageRoot, userDir)
	absolutePath := filepath.Join(directory, fileName)
	data, err := domain.DecodeBase64(audio.Base64)
	if err != nil || len(data) <= 0 || len(data) > domain.MaxAudioUploadBytes {
		return savedAudioFile{}, errors.New("audio payload is invalid")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return savedAudioFile{}, err
	}
	if err := os.WriteFile(absolutePath, data, 0o644); err != nil {
		return savedAudioFile{}, err
	}
	return savedAudioFile{publicURL: publicBase + "/" + userDir + "/" + fileName, absolutePath: absolutePath}, nil
}

type quotaHTTPError struct {
	status  int
	message string
}

func recordingQuotaError(q quota.RecordingQuota, duration int) *quotaHTTPError {
	if q.IsSubscriber {
		if duration > domain.SubscriberMaxSessionSeconds {
			return &quotaHTTPError{status: http.StatusBadRequest, message: "Subscribers can save recordings up to 10:00 per session."}
		}
		return nil
	}
	remaining := 0
	if q.WeeklyRemainingSeconds != nil {
		remaining = *q.WeeklyRemainingSeconds
	}
	if duration > remaining {
		return &quotaHTTPError{
			status:  http.StatusForbidden,
			message: "Weekly free limit exceeded. You have " + domain.FormatSeconds(remaining) + " left out of " + domain.FormatSeconds(domain.FreeWeeklyLimitSeconds) + " this week.",
		}
	}
	return nil
}

func recordingQuotaAfterSave(before quota.RecordingQuota, duration int) quota.RecordingQuota {
	after := before
	savedSeconds := domain.ToNonNegativeInt(duration)
	after.WeeklyUsedSeconds = domain.ToNonNegativeInt(before.WeeklyUsedSeconds) + savedSeconds
	if before.WeeklyRemainingSeconds != nil {
		remaining := domain.ToNonNegativeInt(*before.WeeklyRemainingSeconds) - savedSeconds
		if remaining < 0 {
			remaining = 0
		}
		after.WeeklyRemainingSeconds = &remaining
	}
	return after
}

func stringOrNil(condition bool, value *string) *string {
	if !condition {
		return nil
	}
	return value
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func chooseString(condition bool, ifTrue string, ifFalse string) string {
	if condition {
		return ifTrue
	}
	return ifFalse
}
