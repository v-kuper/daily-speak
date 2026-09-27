package httpapi

import "daily-speaking-practice/backend/internal/recording"

func learningReferenceFor(ruleID string, category suggestionCategory) *learningReference {
	return recording.ReferenceFor(ruleID, category)
}
