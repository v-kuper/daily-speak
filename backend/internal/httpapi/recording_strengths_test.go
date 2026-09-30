package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestStrengthsRouteRequiresPOSTAndAuthentication(t *testing.T) {
	server := newTestServer(Config{})
	for _, test := range []struct {
		method string
		status int
		code   string
	}{
		{http.MethodGet, http.StatusMethodNotAllowed, "method_not_allowed"},
		{http.MethodPost, http.StatusUnauthorized, "invalid_access_token"},
	} {
		response := httptest.NewRecorder()
		server.routeRecordingV1(response, httptest.NewRequest(test.method, "/api/v1/recordings/recording/strengths", nil), "recording/strengths")
		assertV1ErrorCode(t, response, test.status, test.code)
	}
}

func TestStrengthsV1RetryIsOwnedIdempotentAndIndependent(t *testing.T) {
	fixture := newRecordingCreateV1Fixture(t)
	ctx := context.Background()
	id := uuid.NewString()
	if _, err := fixture.database.Exec(ctx, `INSERT INTO recordings
		(id, user_id, topic, duration, timestamp, status, transcript, corrected_transcript, suggestions, strengths_status, shadowing_status)
		VALUES ($1, $2, 'Travel', 20, NOW(), 'ready', 'I go home.', 'I went home.',
		'[{"wrong":"go","right":"went","explanation":"Past time."}]'::jsonb, 'failed', 'ready')`, id, fixture.owner.Identity.User.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = fixture.database.Exec(context.Background(), `DELETE FROM processing_jobs WHERE resource_id = $1`, id)
	})
	post := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/recordings/"+id+"/strengths", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		fixture.server.routeRecordingV1(response, request, id+"/strengths")
		return response
	}
	assertV1ErrorCode(t, post(fixture.other.AccessToken), http.StatusNotFound, "recording_not_found")
	assertV1ErrorCode(t, post(fixture.guest.AccessToken), http.StatusForbidden, "account_required")
	for _, scheduled := range []bool{true, false} {
		response := post(fixture.owner.AccessToken)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var payload struct {
			Recording recordingV1Response `json:"recording"`
			Scheduled bool                `json:"scheduled"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Scheduled != scheduled || payload.Recording.Status != "ready" || payload.Recording.StrengthsStatus != "processing" || payload.Recording.ShadowingStatus != "ready" || payload.Recording.CorrectedTranscript != "I went home." || len(payload.Recording.Suggestions) != 1 {
			t.Fatalf("payload=%#v", payload)
		}
	}
	if _, err := fixture.database.Exec(ctx, `UPDATE recordings SET status = 'failed', processing_stage = 'suggestions', strengths_status = 'failed' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	assertV1ErrorCode(t, post(fixture.owner.AccessToken), http.StatusConflict, "strengths_not_ready")
}
