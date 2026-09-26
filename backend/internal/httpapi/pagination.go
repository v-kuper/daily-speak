package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

var errInvalidPagination = errors.New("invalid pagination parameters")

type pageCursor struct {
	Version   int       `json:"v"`
	Timestamp time.Time `json:"t"`
	ID        string    `json:"id"`
}

type pageRequest struct {
	Limit  int
	Cursor *pageCursor
}

type pageInfo struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"nextCursor"`
}

func parsePageRequest(r *http.Request) (pageRequest, error) {
	values := r.URL.Query()
	page := pageRequest{Limit: defaultPageLimit}
	if raw, ok := values["limit"]; ok {
		if len(raw) != 1 {
			return pageRequest{}, errInvalidPagination
		}
		limit, err := strconv.Atoi(strings.TrimSpace(raw[0]))
		if err != nil || limit < 1 || limit > maxPageLimit {
			return pageRequest{}, errInvalidPagination
		}
		page.Limit = limit
	}
	if raw, ok := values["cursor"]; ok {
		if len(raw) != 1 || raw[0] == "" || len(raw[0]) > 2048 {
			return pageRequest{}, errInvalidPagination
		}
		cursor, err := decodePageCursor(raw[0])
		if err != nil {
			return pageRequest{}, errInvalidPagination
		}
		page.Cursor = &cursor
	}
	return page, nil
}

func encodePageCursor(cursor pageCursor) (string, error) {
	cursor.Version = 1
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodePageCursor(value string) (pageCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return pageCursor{}, errInvalidPagination
	}
	var cursor pageCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return pageCursor{}, errInvalidPagination
	}
	if cursor.Version != 1 || cursor.Timestamp.IsZero() || strings.TrimSpace(cursor.ID) == "" || len(cursor.ID) > 200 {
		return pageCursor{}, errInvalidPagination
	}
	return cursor, nil
}
