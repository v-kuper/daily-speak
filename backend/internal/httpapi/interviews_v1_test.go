package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"daily-speaking-practice/backend/internal/interview"
)

func TestWriteInterviewErrorDistinguishesGuestPreviewAndDurationLimits(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		message string
	}{
		{
			name:    "guest preview",
			err:     interview.ErrQuota,
			message: "This guest identity has already used its single interview preview",
		},
		{
			name:    "duration",
			err:     interview.ErrDurationLimit,
			message: "Interview duration limit is exhausted",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/interviews", nil)
			(&Server{}).writeInterviewError(response, request, test.err)

			var body v1ErrorEnvelope
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusForbidden || body.Error.Code != "quota_exceeded" || body.Error.Message != test.message {
				t.Fatalf("response=%d body=%+v", response.Code, body)
			}
		})
	}
}

func TestWriteInterviewErrorDistinguishesQuestionSpeechFailure(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/interviews/session/question-speech-token", nil)
	(&Server{}).writeInterviewError(response, request, interview.ErrSpeechUnavailable)

	var body v1ErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusServiceUnavailable || body.Error.Code != "interview_speech_unavailable" ||
		body.Error.Message != "Question audio is unavailable" {
		t.Fatalf("response=%d body=%+v", response.Code, body)
	}
}
