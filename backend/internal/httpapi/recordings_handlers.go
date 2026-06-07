package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/ai"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/quota"
	"daily-speaking-practice/backend/internal/transcription"
	"github.com/google/uuid"
)

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

	interestRows, err := s.db.Query(r.Context(), `
		SELECT interest_id
		FROM user_interests
		WHERE user_id = $1
		ORDER BY created_at ASC`, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	interests := []string{}
	for interestRows.Next() {
		var interest string
		if err := interestRows.Scan(&interest); err == nil {
			interests = append(interests, interest)
		}
	}
	interestRows.Close()
	if len(interests) > 10 {
		interests = interests[:10]
	}

	transcript, err := transcription.TranscribeAudioWithLocalWhisper(r.Context(), savedAudio.absolutePath)
	if err != nil {
		var typed transcription.Error
		if errors.As(err, &typed) {
			writeJSON(w, typed.Status, map[string]string{"error": typed.Message})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	transcript = domain.NormalizeTranscript(transcript)
	if transcript == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Whisper returned an empty transcript. Try speaking louder or recording again."})
		return
	}

	suggestions := s.generateRecordingSuggestions(r, transcript, topic, interests, practiceType, photoObject, logger)
	suggestionJSON, _ := json.Marshal(suggestions)
	timestamp := domain.ParseTimestamp(stringAny(source["timestamp"]))

	var inserted struct {
		ID           string
		Topic        string
		Duration     int
		Timestamp    time.Time
		Transcript   string
		Suggestions  []byte
		PracticeType string
		AudioDataURL *string
		PhotoDataURL *string
		PhotoObject  *string
	}
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO recordings
		  (id, user_id, topic, duration, timestamp, transcript, suggestions, practice_type, audio_data_url, photo_data_url, photo_object)
		VALUES
		  ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11)
		RETURNING id, topic, duration, timestamp, transcript, suggestions, practice_type, audio_data_url, photo_data_url, photo_object`,
		recordingID,
		user.ID,
		truncateRunes(topic, 300),
		duration,
		timestamp,
		transcript,
		string(suggestionJSON),
		practiceType,
		savedAudio.publicURL,
		stringOrNil(practiceType == "photo_description", photoDataURL),
		stringOrNil(practiceType == "photo_description", photoObject),
	).Scan(&inserted.ID, &inserted.Topic, &inserted.Duration, &inserted.Timestamp, &inserted.Transcript, &inserted.Suggestions, &inserted.PracticeType, &inserted.AudioDataURL, &inserted.PhotoDataURL, &inserted.PhotoObject)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	cleanupAudio = ""

	q, err := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &user.IsSubscriber)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save recording."})
		return
	}
	recording := recordingResponse{
		ID:           inserted.ID,
		Topic:        inserted.Topic,
		Duration:     domain.ToNonNegativeInt(inserted.Duration),
		Timestamp:    inserted.Timestamp.UTC().Format(time.RFC3339Nano),
		Transcript:   inserted.Transcript,
		Suggestions:  normalizeSuggestions(inserted.Suggestions, 8),
		PracticeType: domain.NormalizePracticeType(inserted.PracticeType),
		AudioDataURL: normalizeOptionalAudio(inserted.AudioDataURL, true),
		PhotoDataURL: normalizeOptionalPhoto(inserted.PhotoDataURL),
		PhotoObject:  normalizeOptionalPhotoObject(inserted.PhotoObject),
	}
	logger.Info("request.success", map[string]any{"status": 201, "durationMs": logging.ElapsedMs(started), "userId": user.ID, "recordingId": recording.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"recording": recording, "quota": q})
}

func (s *Server) generateRecordingSuggestions(r *http.Request, transcript string, topic string, interests []string, practiceType string, photoObject *string, logger logging.Logger) []suggestion {
	if strings.TrimSpace(transcript) == "" {
		return []suggestion{}
	}
	settings := ai.ResolveSettingsForUser()
	useJSONFormat := !settings.IsThinkingModel
	seed := absMod(domain.HashString(strings.ToLower(topic))*131+domain.HashString(transcript)*17, 2147483647)
	prompt := recordingSuggestionsPrompt(transcript, topic, interests, practiceType, photoObject)
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
		payload, _, err := ai.PostChat(r.Context(), body)
		if err != nil {
			logger.Warn("ollama.suggestions_request_failed", logging.ErrorMeta(err))
			return []suggestion{}
		}
		suggestions := parseSuggestionsFromContent(ai.ExtractMessageContent(payload))
		if len(suggestions) > 0 {
			if len(suggestions) > 5 {
				return suggestions[:5]
			}
			return suggestions
		}
	}
	return []suggestion{}
}

func parseSuggestionsFromContent(content string) []suggestion {
	for _, candidate := range ai.ExtractJSONCandidates(content) {
		var payload map[string]json.RawMessage
		if json.Unmarshal([]byte(candidate), &payload) != nil {
			continue
		}
		for _, key := range []string{"suggestions", "corrections", "mistakes", "errorAnalysis"} {
			if raw, ok := payload[key]; ok {
				suggestions := normalizeSuggestions(raw, 5)
				if len(suggestions) > 0 {
					return suggestions
				}
			}
		}
	}
	return []suggestion{}
}

func recordingSuggestionsPrompt(transcript string, topic string, interests []string, practiceType string, photoObject *string) string {
	transcriptForPrompt := domain.NormalizeTranscript(transcript)
	if len([]rune(transcriptForPrompt)) > 6000 {
		transcriptForPrompt = string([]rune(transcriptForPrompt)[:6000])
	}
	parts := []string{
		`Topic: "` + topic + `".`,
		"You receive an English learner transcript from a speaking practice recording.",
		"Find up to 4 grammar or word-choice mistakes that clearly appear in the transcript.",
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
