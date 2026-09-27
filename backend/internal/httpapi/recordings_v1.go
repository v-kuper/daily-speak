package httpapi

import (
	"net/http"

	"daily-speaking-practice/backend/internal/recording"
)

func (s *Server) handleListRecordingsV1(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.v1.recordings.list")
	if !ok {
		return
	}
	page, err := parsePageRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid pagination parameters."})
		return
	}
	options := recording.ListOptions{Limit: page.Limit + 1}
	if page.Cursor != nil {
		options.BeforeTimestamp = &page.Cursor.Timestamp
		options.BeforeID = page.Cursor.ID
	}
	records, err := s.recordingReader.List(r.Context(), user.ID, options)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load recordings."})
		return
	}
	var nextCursor *string
	if len(records) > page.Limit {
		last := records[page.Limit-1]
		encoded, err := encodePageCursor(pageCursor{Timestamp: last.Timestamp, ID: last.ID})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to paginate recordings."})
			return
		}
		nextCursor = &encoded
		records = records[:page.Limit]
	}
	responses := make([]recordingResponse, 0, len(records))
	for _, record := range records {
		responses = append(responses, recordingResponseFromRecord(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": responses,
		"page":  pageInfo{Limit: page.Limit, NextCursor: nextCursor},
	})
}
