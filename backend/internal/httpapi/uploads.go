package httpapi

import (
	"net/http"
	"strings"

	"daily-speaking-practice/backend/internal/media"
)

const uploadsURLPrefix = "/uploads/"

func (s *Server) handleLegacyUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	// Only the two historical public URL shapes remain available here.
	// New v1 objects, multipart state and metadata share the mounted local
	// root but must only be reachable through owner-checked signed routes.
	segments := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, uploadsURLPrefix), "/"), "/")
	if len(segments) != 3 || (segments[0] != "recordings" && segments[0] != "feed-replies") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	absolutePath, err := s.legacyUploads.Path(r.URL.Path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	http.ServeFile(w, r, absolutePath)
}

func (s *Server) handleShadowingUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	publicURL := media.NormalizeStoredShadowingAudioSource(r.URL.Path)
	if publicURL == nil || *publicURL != r.URL.Path {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	user, ok := s.authorizedUser(w, r, "uploads.shadowing.get")
	if !ok {
		return
	}

	owned, err := s.recordingReader.OwnsLegacyShadowing(r.Context(), user.ID, *publicURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load pronunciation audio."})
		return
	}
	if !owned {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	absolutePath, err := s.legacyUploads.Path(*publicURL)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, absolutePath)
}
