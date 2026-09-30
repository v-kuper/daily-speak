package httpapi

import (
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/recording"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFocusedRoutesRequireBearerBeforeFeatureAccess(t *testing.T) {
	handler := newTestServer(Config{}).Handler()
	for _, path := range []string{
		"/api/v1/recordings/r/feedback/f/audio",
		"/api/v1/recordings/r/feedback/reanalyze",
		"/api/v1/recordings/r/interview-turns/1/question-audio",
		"/api/v1/recordings/r/interview-turns/1/attempts",
		"/api/v1/recordings/r/interview-turns/1/attempts/a",
		"/api/v1/recordings/r/interview-turns/1/attempts/a/retry",
		"/api/v1/recordings/r/interview-turns/1/attempts/a/feedback/f/audio",
		"/api/v1/interviews/s/questions/1/audio",
	} {
		method := http.MethodPost
		if path == "/api/v1/recordings/r/interview-turns/1/attempts/a" {
			method = http.MethodGet
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		assertV1ErrorCode(t, response, http.StatusUnauthorized, "invalid_access_token")
	}
}
func TestArtifactAudioResponseHasPrivateStateAndNoObjectKey(t *testing.T) {
	signer, err := media.NewURLSigner([]byte("a-test-signing-secret-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{mediaSigner: signer}
	download := &media.Download{Local: true, Asset: media.Asset{ID: "asset", State: "ready", ObjectKey: "sessions/private/questions/1.mp3"}, Request: media.SignedRequest{Method: "GET", ExpiresAt: time.Now().Add(time.Minute), Headers: map[string][]string{}}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/audio", nil)
	server.audioResponse(response, request, "ready", "", download, 1)
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	asset := body["asset"].(map[string]any)
	signed := body["request"].(map[string]any)
	if body["status"] != "ready" || body["questionIndex"] != float64(1) || asset["objectKey"] != nil || signed["url"] == "" || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("response=%+v", body)
	}
}
func TestFocusedErrorMappingsAreStable(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{recording.ErrNotFound, 404, "not_found"}, {recording.ErrFeedbackInvalid, 400, "invalid_request"}, {recording.ErrFeedbackConflict, 409, "feedback_not_ready"}} {
		response := httptest.NewRecorder()
		new(Server).writeFocusedError(response, httptest.NewRequest("POST", "/feedback", nil), test.err)
		assertV1ErrorCode(t, response, test.status, test.code)
	}
}
