package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/activity"
)

func TestActivityRouteRequiresAuthenticationAndRejectsUnsupportedMethods(t *testing.T) {
	handler := newTestServer(Config{}).Handler()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/profile/activity", strings.NewReader(`{"intervals":[]}`)))
		expected := http.StatusUnauthorized
		if method == http.MethodDelete {
			expected = http.StatusMethodNotAllowed
		}
		if response.Code != expected || response.Header().Get(requestIDHeader) == "" {
			t.Fatalf("%s status=%d body=%s", method, response.Code, response.Body.String())
		}
	}
}

func TestActivityErrorMapping(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{activity.ErrInvalid, 400, "invalid_activity"}, {activity.ErrConflict, 409, "activity_conflict"}, {activity.ErrAccount, 403, "account_required"}} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/profile/activity", nil)
		(&Server{}).writeActivityError(response, request, test.err)
		var body v1ErrorEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != test.status || body.Error.Code != test.code {
			t.Fatalf("status=%d body=%+v", response.Code, body)
		}
	}
}
