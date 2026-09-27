package httpapi

import (
	"net/url"
	"strconv"
	"strings"

	"daily-speaking-practice/backend/internal/learner"
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

func normalizeURLInterests(values url.Values) []string {
	return learner.NormalizeInterests(values["interest"], 10)
}

func errorMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}

func pathUnescape(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}
