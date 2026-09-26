package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/auth"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/storage"
)

const maxMediaRequestBytes = 128 << 10

func (s *Server) routeMediaV1(w http.ResponseWriter, r *http.Request, path string) bool {
	switch {
	case path == "/api/v1/media/uploads" && r.Method == http.MethodPost:
		s.handleCreateMediaUploadV1(w, r)
		return true
	case path == "/api/v1/media/uploads":
		writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return true
	case strings.HasPrefix(path, "/api/v1/media/uploads/"):
		s.routeMediaUploadV1(w, r, strings.TrimPrefix(path, "/api/v1/media/uploads/"))
		return true
	case strings.HasPrefix(path, "/api/v1/media/") && strings.HasSuffix(path, "/download"):
		assetID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/media/"), "/download")
		if assetID == "" || strings.Contains(assetID, "/") {
			writeV1Error(w, r, http.StatusNotFound, "not_found", "Media not found")
			return true
		}
		if r.Method != http.MethodGet {
			writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return true
		}
		s.handleMediaDownloadV1(w, r, assetID)
		return true
	}
	return false
}

func (s *Server) routeMediaUploadV1(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		switch r.Method {
		case http.MethodGet:
			s.handleGetMediaUploadV1(w, r, parts[0])
		case http.MethodDelete:
			s.handleAbortMediaUploadV1(w, r, parts[0])
		default:
			writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[0] != "" {
		switch {
		case parts[1] == "parts" && r.Method == http.MethodPost:
			s.handleSignMediaUploadPartsV1(w, r, parts[0])
		case parts[1] == "complete" && r.Method == http.MethodPost:
			s.handleCompleteMediaUploadV1(w, r, parts[0])
		default:
			writeV1Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
		return
	}
	writeV1Error(w, r, http.StatusNotFound, "not_found", "Media upload not found")
}

func (s *Server) handleCreateMediaUploadV1(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requiredMediaUserV1(w, r)
	if !ok || !s.mediaAvailable(w, r) {
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		writeV1Error(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key is required")
		return
	}
	var payload struct {
		Purpose     string `json:"purpose"`
		ContentType string `json:"contentType"`
		SizeBytes   int64  `json:"sizeBytes"`
		Checksum    struct {
			Algorithm string `json:"algorithm"`
			Value     string `json:"value"`
		} `json:"checksum"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Checksum.Algorithm), "sha256") {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_checksum", "checksum.algorithm must be sha256")
		return
	}
	resource, err := s.mediaService.CreateUpload(r.Context(), media.CreateUploadInput{
		OwnerPrincipalID: identity.PrincipalID, SessionID: identity.SessionID,
		IdempotencyKey: r.Header.Get("Idempotency-Key"), Purpose: payload.Purpose,
		ContentType: payload.ContentType, SizeBytes: payload.SizeBytes,
		ChecksumSHA256: payload.Checksum.Value,
	})
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/media/uploads/"+resource.Upload.ID)
	writeJSON(w, http.StatusCreated, mediaUploadResponse(resource, nil))
}

func (s *Server) handleGetMediaUploadV1(w http.ResponseWriter, r *http.Request, uploadID string) {
	identity, ok := s.requiredMediaUserV1(w, r)
	if !ok || !s.mediaAvailable(w, r) {
		return
	}
	resource, parts, err := s.mediaService.GetUpload(r.Context(), identity.PrincipalID, uploadID)
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mediaUploadResponse(resource, parts))
}

func (s *Server) handleSignMediaUploadPartsV1(w http.ResponseWriter, r *http.Request, uploadID string) {
	identity, ok := s.requiredMediaUserV1(w, r)
	if !ok || !s.mediaAvailable(w, r) {
		return
	}
	var payload struct {
		Parts []media.PartDescriptor `json:"parts"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	parts, err := s.mediaService.PresignParts(r.Context(), identity.PrincipalID, uploadID, payload.Parts)
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	response := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		request := part.Request
		if part.Local {
			if s.mediaSigner == nil {
				s.writeMediaError(w, r, media.ErrStorage)
				return
			}
			path := "/api/v1/media/uploads/" + url.PathEscape(uploadID) + "/parts/" + strconv.Itoa(part.Descriptor.PartNumber)
			values := url.Values{
				"sizeBytes":      {strconv.FormatInt(part.Descriptor.SizeBytes, 10)},
				"checksumSha256": {part.Descriptor.ChecksumSHA256},
			}
			request.URL = s.mediaSigner.Sign(http.MethodPut, path, values, request.ExpiresAt)
		}
		response = append(response, map[string]any{
			"partNumber": part.Descriptor.PartNumber, "sizeBytes": part.Descriptor.SizeBytes,
			"checksumSha256": part.Descriptor.ChecksumSHA256,
			"request":        mediaRequestResponse(request),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"parts": response})
}

func (s *Server) handleCompleteMediaUploadV1(w http.ResponseWriter, r *http.Request, uploadID string) {
	identity, ok := s.requiredMediaUserV1(w, r)
	if !ok || !s.mediaAvailable(w, r) {
		return
	}
	var payload struct {
		Parts []media.CompletedPart `json:"parts"`
	}
	if !decodeMediaJSON(w, r, &payload) {
		return
	}
	resource, err := s.mediaService.CompleteUpload(r.Context(), identity.PrincipalID, uploadID, payload.Parts)
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mediaUploadResponse(resource, nil))
}

func (s *Server) handleAbortMediaUploadV1(w http.ResponseWriter, r *http.Request, uploadID string) {
	identity, ok := s.requiredMediaUserV1(w, r)
	if !ok || !s.mediaAvailable(w, r) {
		return
	}
	if _, err := s.mediaService.AbortUpload(r.Context(), identity.PrincipalID, uploadID); err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMediaDownloadV1(w http.ResponseWriter, r *http.Request, assetID string) {
	identity, ok := s.requiredMediaUserV1(w, r)
	if !ok || !s.mediaAvailable(w, r) {
		return
	}
	download, err := s.mediaService.Download(r.Context(), identity.PrincipalID, assetID)
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	request := download.Request
	if download.Local {
		if s.mediaSigner == nil {
			s.writeMediaError(w, r, media.ErrStorage)
			return
		}
		path := "/api/v1/media/local/assets/" + url.PathEscape(assetID) + "/content"
		request.URL = s.mediaSigner.Sign(http.MethodGet, path, nil, request.ExpiresAt)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asset": mediaAssetResponse(download.Asset), "request": mediaRequestResponse(request),
	})
}

func (s *Server) routeSignedLocalMedia(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.mediaService == nil || s.mediaSigner == nil || !s.mediaSigner.Verify(r.Method, r.URL.Path, r.URL.Query()) {
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Media not found")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/media/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 4 && parts[0] == "uploads" && parts[2] == "parts" && r.Method == http.MethodPut {
		partNumber, partErr := strconv.Atoi(parts[3])
		sizeBytes, sizeErr := strconv.ParseInt(r.URL.Query().Get("sizeBytes"), 10, 64)
		if partErr != nil || sizeErr != nil || sizeBytes <= 0 || r.ContentLength != sizeBytes {
			writeV1Error(w, r, http.StatusBadRequest, "invalid_part", "Media part is invalid")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, sizeBytes)
		part, err := s.mediaService.PutLocalPart(r.Context(), parts[1], media.PartDescriptor{
			PartNumber: partNumber, SizeBytes: sizeBytes,
			ChecksumSHA256: r.URL.Query().Get("checksumSha256"),
		}, sizeBytes, r.Body)
		if err != nil {
			s.writeMediaError(w, r, err)
			return
		}
		w.Header().Set("ETag", part.ETag)
		w.Header().Set("X-Checksum-SHA256", part.SHA256)
		w.WriteHeader(http.StatusOK)
		return
	}
	if len(parts) == 4 && parts[0] == "local" && parts[1] == "assets" && parts[3] == "content" && r.Method == http.MethodGet {
		content, err := s.mediaService.OpenSignedContent(r.Context(), parts[2])
		if err != nil {
			s.writeMediaError(w, r, err)
			return
		}
		defer content.Body.Close()
		w.Header().Set("Content-Type", content.Info.ContentType)
		w.Header().Set("Content-Length", strconv.FormatInt(content.Info.Size, 10))
		w.Header().Set("ETag", content.Info.ETag)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if seeker, ok := content.Body.(io.ReadSeeker); ok {
			http.ServeContent(w, r, "", content.Info.LastModified, seeker)
			return
		}
		_, _ = io.Copy(w, content.Body)
		return
	}
	writeV1Error(w, r, http.StatusNotFound, "not_found", "Media not found")
}

func (s *Server) routeMediaUploadEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		s.routeSignedLocalMedia(w, r)
		return
	}
	s.routeV1(w, r)
}

func (s *Server) requiredMediaUserV1(w http.ResponseWriter, r *http.Request) (*auth.Identity, bool) {
	identity, ok := s.requiredIdentityV1(w, r)
	if !ok {
		return nil, false
	}
	if identity.Kind != "user" || identity.User == nil {
		writeV1Error(w, r, http.StatusForbidden, "account_required", "An account is required for media")
		return nil, false
	}
	return identity, true
}

func (s *Server) mediaAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.mediaService == nil || !s.mediaService.Available() {
		writeV1Error(w, r, http.StatusServiceUnavailable, "storage_unavailable", "Media storage is unavailable")
		return false
	}
	return true
}

func decodeMediaJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	if r.Body == nil {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "A JSON request body is required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeV1Error(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "Request body is too large")
		} else {
			writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Request body must be valid JSON")
		}
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Request body must contain one JSON object")
		return false
	}
	return true
}

func (s *Server) writeMediaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, media.ErrNotFound):
		writeV1Error(w, r, http.StatusNotFound, "not_found", "Media not found")
	case errors.Is(err, media.ErrExpired):
		writeV1Error(w, r, http.StatusGone, "upload_expired", "Media upload has expired")
	case errors.Is(err, media.ErrConflict):
		writeV1Error(w, r, http.StatusConflict, "conflict", "Media upload conflicts with its current state")
	case errors.Is(err, media.ErrInvalidRequest):
		writeV1Error(w, r, http.StatusBadRequest, "invalid_request", "Media request is invalid")
	case errors.Is(err, media.ErrUnsupportedType):
		writeV1Error(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Media type is unsupported")
	case errors.Is(err, media.ErrPayloadTooLarge):
		writeV1Error(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "Media payload is too large")
	case errors.Is(err, media.ErrChecksumMismatch):
		writeV1Error(w, r, http.StatusUnprocessableEntity, "checksum_mismatch", "Media checksum does not match")
	case errors.Is(err, media.ErrSizeMismatch):
		writeV1Error(w, r, http.StatusUnprocessableEntity, "size_mismatch", "Media size does not match")
	default:
		writeV1Error(w, r, http.StatusServiceUnavailable, "storage_unavailable", "Media storage is unavailable")
	}
}

func mediaUploadResponse(resource media.UploadResource, parts []storage.PartInfo) map[string]any {
	uploaded := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		uploaded = append(uploaded, map[string]any{
			"partNumber": part.Number, "sizeBytes": part.Size,
			"etag": part.ETag, "checksumSha256": part.SHA256,
		})
	}
	return map[string]any{
		"asset": mediaAssetResponse(resource.Asset),
		"upload": map[string]any{
			"id": resource.Upload.ID, "state": resource.Upload.State,
			"partSizeBytes": resource.Upload.PartSizeBytes, "partCount": resource.Upload.PartCount,
			"expiresAt":     resource.Upload.ExpiresAt.UTC().Format(time.RFC3339Nano),
			"uploadedParts": uploaded,
		},
	}
}

func mediaAssetResponse(asset media.Asset) map[string]any {
	return map[string]any{
		"id": asset.ID, "state": asset.State, "purpose": asset.Purpose,
		"contentType": asset.ContentType, "sizeBytes": asset.ExpectedSizeBytes,
		"checksum": map[string]string{"algorithm": "sha256", "value": asset.ExpectedChecksumSHA256},
	}
}

func mediaRequestResponse(request storage.PresignedRequest) map[string]any {
	headers := make(map[string]string, len(request.Headers))
	for name, values := range request.Headers {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}
	return map[string]any{
		"method": request.Method, "url": request.URL, "headers": headers,
		"expiresAt": request.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
}
