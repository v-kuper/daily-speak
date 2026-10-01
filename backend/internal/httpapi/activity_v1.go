package httpapi

import (
	"errors"
	"net/http"
	"time"

	"daily-speaking-practice/backend/internal/activity"
)

type activityIntervalJSON struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
}

type activityDayJSON struct {
	Date                 string `json:"date"`
	SpeakingMilliseconds int64  `json:"speakingMilliseconds"`
	ReviewMilliseconds   int64  `json:"reviewMilliseconds"`
	Level                int    `json:"level"`
}

type activitySummaryJSON struct {
	TotalSpeakingMilliseconds      int64             `json:"totalSpeakingMilliseconds"`
	HistoricalSpeakingMilliseconds int64             `json:"historicalSpeakingMilliseconds"`
	Timezone                       string            `json:"timezone"`
	From                           string            `json:"from"`
	To                             string            `json:"to"`
	Days                           []activityDayJSON `json:"days"`
}

func (s *Server) handleActivitySummary(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	if s.activityService == nil {
		s.writeActivityError(w, r, errors.New("unconfigured"))
		return
	}
	summary, err := s.activityService.Summary(r.Context(), identity.User.ID, r.URL.Query().Get("timezone"))
	if err != nil {
		s.writeActivityError(w, r, err)
		return
	}
	response := activitySummaryJSON{
		TotalSpeakingMilliseconds:      summary.TotalSpeakingMilliseconds,
		HistoricalSpeakingMilliseconds: summary.HistoricalSpeakingMilliseconds,
		Timezone:                       summary.Timezone, From: summary.From, To: summary.To,
		Days: make([]activityDayJSON, 0, len(summary.Days)),
	}
	for _, day := range summary.Days {
		response.Days = append(response.Days, activityDayJSON{day.Date, day.SpeakingMilliseconds, day.ReviewMilliseconds, day.Level})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleActivityRecord(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredAccountIdentityV1(w, r)
	if !ok {
		return
	}
	var payload struct {
		Intervals []activityIntervalJSON `json:"intervals"`
	}
	if !decodeIdentityJSON(w, r, &payload) {
		return
	}
	if s.activityService == nil {
		s.writeActivityError(w, r, errors.New("unconfigured"))
		return
	}
	intervals := make([]activity.Interval, 0, len(payload.Intervals))
	for _, item := range payload.Intervals {
		intervals = append(intervals, activity.Interval{ID: item.ID, Kind: item.Kind, StartedAt: item.StartedAt, EndedAt: item.EndedAt})
	}
	if err := s.activityService.Record(r.Context(), identity.User.ID, intervals); err != nil {
		s.writeActivityError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) writeActivityError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, activity.ErrInvalid):
		writeV1Error(w, r, http.StatusBadRequest, "invalid_activity", "Activity intervals or timezone are invalid")
	case errors.Is(err, activity.ErrConflict):
		writeV1Error(w, r, http.StatusConflict, "activity_conflict", "Activity key was already used")
	case errors.Is(err, activity.ErrAccount):
		writeV1Error(w, r, http.StatusForbidden, "account_required", "An account is required")
	default:
		writeV1Error(w, r, http.StatusInternalServerError, "activity_unavailable", "Failed to load or save activity")
	}
}
