package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/recording"
)

type suggestionCategory = recording.SuggestionCategory

type suggestionSeverity = recording.SuggestionSeverity

const (
	categoryLanguageSwitch    = recording.CategoryLanguageSwitch
	categoryVerbGrammar       = recording.CategoryVerbGrammar
	categoryNounsDeterminers  = recording.CategoryNounsDeterminers
	categoryPrepositions      = recording.CategoryPrepositions
	categoryVocabulary        = recording.CategoryVocabulary
	categorySentenceStructure = recording.CategorySentenceStructure
	categoryNaturalness       = recording.CategoryNaturalness

	severityMajor  = recording.SeverityMajor
	severityMedium = recording.SeverityMedium
	severityMinor  = recording.SeverityMinor
)

type learningReference = recording.LearningReference

type suggestion = recording.Suggestion

type recordingResponse struct {
	ID                  string                  `json:"id"`
	Topic               string                  `json:"topic"`
	Duration            int                     `json:"duration"`
	Timestamp           string                  `json:"timestamp"`
	Status              string                  `json:"status"`
	Transcript          string                  `json:"transcript"`
	CorrectedTranscript string                  `json:"correctedTranscript"`
	Suggestions         []suggestion            `json:"suggestions"`
	ProcessingStage     *string                 `json:"processingStage"`
	PracticeType        string                  `json:"practiceType"`
	AudioDataURL        *string                 `json:"audioDataUrl"`
	PhotoDataURL        *string                 `json:"photoDataUrl"`
	PhotoObject         *string                 `json:"photoObject"`
	ProcessingError     *string                 `json:"processingError"`
	ShadowingStatus     string                  `json:"shadowingStatus"`
	ShadowingAudioURL   *string                 `json:"shadowingAudioUrl"`
	ShadowingError      *string                 `json:"shadowingError"`
	ShadowingUpdatedAt  string                  `json:"shadowingUpdatedAt"`
	Media               *recordingMediaResponse `json:"media,omitempty"`
}

type recordingMediaResponse struct {
	Audio     *recordingMediaAssetResponse `json:"audio,omitempty"`
	Photo     *recordingMediaAssetResponse `json:"photo,omitempty"`
	Shadowing *recordingMediaAssetResponse `json:"shadowing,omitempty"`
}

type recordingMediaAssetResponse struct {
	AssetID      string `json:"assetId"`
	DownloadPath string `json:"downloadPath"`
}

func recordingMedia(audioAssetID, photoAssetID, shadowingAssetID *string) *recordingMediaResponse {
	response := &recordingMediaResponse{
		Audio:     recordingMediaAsset(audioAssetID),
		Photo:     recordingMediaAsset(photoAssetID),
		Shadowing: recordingMediaAsset(shadowingAssetID),
	}
	if response.Audio == nil && response.Photo == nil && response.Shadowing == nil {
		return nil
	}
	return response
}

func recordingMediaAsset(assetID *string) *recordingMediaAssetResponse {
	if assetID == nil || strings.TrimSpace(*assetID) == "" {
		return nil
	}
	id := strings.TrimSpace(*assetID)
	return &recordingMediaAssetResponse{
		AssetID:      id,
		DownloadPath: "/api/v1/media/" + url.PathEscape(id) + "/download",
	}
}

func (s *Server) optionalUser(r *http.Request) (*auth.User, error) {
	token := sessionToken(r)
	if token == "" {
		return nil, nil
	}
	return auth.GetUserBySessionToken(r.Context(), s.db, token)
}

func decodeJSON(r *http.Request, dest any) bool {
	if r.Body == nil {
		return false
	}
	return json.NewDecoder(r.Body).Decode(dest) == nil
}

func parseIntAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func stringAny(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func normalizeSuggestions(input []byte, limit int) []suggestion {
	return recording.NormalizeSuggestions(input, limit)
}

func parseSuggestionCategory(value string) (suggestionCategory, bool) {
	return recording.ParseSuggestionCategory(value)
}

func validSuggestionCategory(category suggestionCategory) bool {
	return recording.ValidSuggestionCategory(category)
}

func parseSuggestionSeverity(value string) (suggestionSeverity, bool) {
	return recording.ParseSuggestionSeverity(value)
}

func validSuggestionSeverity(severity suggestionSeverity) bool {
	return recording.ValidSuggestionSeverity(severity)
}

func withoutLearningReference(item suggestion) suggestion {
	return recording.WithoutLearningReference(item)
}

func normalizeURLInterests(values url.Values) []string {
	return domain.NormalizeInterests(domain.URLQueryAll(values, "interest"), 10)
}

func errorMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}

func chooseFloat(condition bool, ifTrue float64, ifFalse float64) float64 {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func absMod(value int, mod int) int {
	if mod <= 0 {
		return value
	}
	out := value % mod
	if out < 0 {
		return -out
	}
	return out
}
