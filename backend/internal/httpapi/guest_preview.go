package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/guestpreview"
	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/workqueue"
)

const (
	guestPreviewMaxDuration       = guestpreview.MaxDuration
	guestPreviewMaxAudioBytes     = guestpreview.MaxAudioBytes
	guestPreviewRetention         = guestpreview.Retention
	guestPreviewProcessingTimeout = guestpreview.ProcessingTimeout
	defaultGuestPreviewQueueCap   = guestpreview.DefaultQueueCapacity
	guestPreviewRetryAfter        = guestpreview.RetryAfterSeconds
)

var guestPreviewIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

var (
	errGuestPreviewNotFound = guestpreview.ErrNotFound
	errGuestPreviewConflict = guestpreview.ErrConflict
	errGuestPreviewCapacity = guestpreview.ErrCapacity
)

type guestPreviewCreateRequest = guestpreview.CreateRequest
type guestPreview = guestpreview.Preview

func (s *Server) routeGuestPreviewV1(w http.ResponseWriter, r *http.Request, path string) bool {
	switch {
	case path == "/api/v1/guest/previews" && r.Method == http.MethodPost:
		s.handleCreateGuestPreviewV1(w, r)
		return true
	case path == "/api/v1/guest/previews":
		writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return true
	case strings.HasPrefix(path, "/api/v1/guest/previews/"):
		id := strings.TrimPrefix(path, "/api/v1/guest/previews/")
		if id == "" || strings.Contains(id, "/") {
			writeV1Error(w, r, http.StatusNotFound, "not_found", "Guest preview not found")
			return true
		}
		if r.Method != http.MethodGet {
			writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return true
		}
		s.handleGetGuestPreviewV1(w, r, id)
		return true
	default:
		return false
	}
}

func (s *Server) handleCreateGuestPreviewV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredGuestIdentityV1(w, r)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !guestPreviewIdempotencyPattern.MatchString(idempotencyKey) {
		writeV1Error(w, r, http.StatusBadRequest, "idempotency_key_required", "A valid Idempotency-Key is required")
		return
	}
	var payload guestPreviewCreateRequest
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	payload, timestamp, err := normalizeGuestPreviewCreate(payload, time.Now().UTC())
	if err != nil {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	preview, created, err := s.createGuestPreview(r.Context(), identity, idempotencyKey, guestPreviewRequestDigest(payload), payload, timestamp)
	if err != nil {
		s.writeGuestPreviewError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/guest/previews/"+preview.ID)
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"preview": guestPreviewResponse(preview)})
}

func (s *Server) handleGetGuestPreviewV1(w http.ResponseWriter, r *http.Request, previewID string) {
	identity, ok := s.requiredGuestIdentityV1(w, r)
	if !ok {
		return
	}
	preview, err := s.findGuestPreview(r.Context(), identity.PrincipalID, previewID, "")
	if err != nil || preview.State == "promoted" || !preview.ExpiresAt.After(time.Now().UTC()) {
		if err != nil && !errors.Is(err, errGuestPreviewNotFound) {
			s.writeGuestPreviewError(w, r, err)
			return
		}
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Guest preview not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preview": guestPreviewResponse(preview)})
}

func (s *Server) requiredGuestIdentityV1(w http.ResponseWriter, r *http.Request) (*auth.Identity, bool) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return nil, false
	}
	if identity.Kind != "guest" {
		writeV1Error(w, r, http.StatusForbidden, "guest_required", "A guest identity is required")
		return nil, false
	}
	return identity, true
}

func normalizeGuestPreviewCreate(input guestPreviewCreateRequest, now time.Time) (guestPreviewCreateRequest, time.Time, error) {
	return guestpreview.NormalizeCreate(input, now)
}

func guestPreviewRequestDigest(input guestPreviewCreateRequest) string {
	return guestpreview.RequestDigest(input)
}
func guestPreviewQueueCapacity() int { return guestpreview.QueueCapacityFromEnv() }

func (s *Server) createGuestPreview(ctx context.Context, identity *auth.Identity, idempotencyKey, requestDigest string, input guestPreviewCreateRequest, timestamp time.Time) (guestPreview, bool, error) {
	return s.guestPreviewStore.Create(ctx, identity.PrincipalID, idempotencyKey, requestDigest, input, timestamp)
}

func (s *Server) findGuestPreview(ctx context.Context, principalID, previewID, idempotencyKey string) (guestPreview, error) {
	return s.guestPreviewStore.Find(ctx, principalID, previewID, idempotencyKey)
}

func guestPreviewResponse(preview guestPreview) map[string]any {
	response := map[string]any{
		"id": preview.ID, "state": preview.State, "topic": preview.Topic,
		"duration": preview.Duration, "timestamp": preview.RecordingTimestamp.UTC().Format(time.RFC3339Nano),
		"practiceType": preview.PracticeType, "transcript": preview.Transcript,
		"corrections": preview.PreviewCorrections,
		"expiresAt":   preview.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"createdAt":   preview.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt":   preview.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if preview.State == "failed" {
		response["processingError"] = "Preview processing failed. Please try again."
	}
	return response
}

func (s *Server) writeGuestPreviewError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errGuestPreviewNotFound):
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Guest preview or media not found")
	case errors.Is(err, errGuestPreviewConflict), errors.Is(err, media.ErrConflict):
		writeV1Error(w, r, http.StatusConflict, "guest_preview_conflict", "This guest already has a preview")
	case errors.Is(err, errGuestPreviewCapacity):
		w.Header().Set("Retry-After", fmt.Sprintf("%d", guestPreviewRetryAfter))
		writeV1Error(w, r, http.StatusServiceUnavailable, "capacity_exhausted", "Guest preview capacity is temporarily exhausted")
	case errors.Is(err, media.ErrPayloadTooLarge):
		writeV1Error(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "Guest preview audio is too large")
	default:
		logging.ForRequest("api.v1.guest.preview", r).Error("guest_preview.failed", logging.ErrorMeta(err))
		writeV1Error(w, r, http.StatusInternalServerError, "internal_error", "Guest preview request failed")
	}
}

func (s *Server) runGuestPreviewJob(ctx context.Context, job workqueue.Job) error {
	return s.guestPreviewProcessor.Process(ctx, guestpreview.Job{ID: job.ID, ResourceID: job.ResourceID, LeaseToken: job.LeaseToken})
}

func (s *Server) generateGuestPreviewCorrections(ctx context.Context, transcript string) ([]suggestion, error) {
	return s.recordingPreviewAnalyzer.PreviewCorrections(ctx, transcript)
}

func (s *Server) expireGuestPreviews(ctx context.Context) error {
	return s.guestPreviewStore.Expire(ctx)
}
