package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"daily-speaking-practice/backend/internal/media"
)

func TestMediaErrorMapsGuestRestriction(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", nil)
	response := httptest.NewRecorder()

	new(Server).writeMediaError(response, request, media.ErrGuestRestricted)

	assertV1ErrorCode(t, response, http.StatusForbidden, "guest_media_restricted")
}
