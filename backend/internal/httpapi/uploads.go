package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/storage"
)

const uploadsURLPrefix = "/uploads/"

func resolveUploadsDir() string {
	value := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if value != "" {
		return value
	}
	return filepath.Join("public", "uploads")
}

func uploadsHandler() http.Handler {
	legacyFiles := http.StripPrefix(uploadsURLPrefix, http.FileServer(http.Dir(resolveUploadsDir())))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if _, err := storedUploadPath(r.URL.Path); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
			return
		}
		legacyFiles.ServeHTTP(w, r)
	})
}

func (s *Server) handleShadowingUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	publicURL := domain.NormalizeStoredShadowingAudioSource(r.URL.Path)
	if publicURL == nil || *publicURL != r.URL.Path {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	user, ok := s.authorizedUser(w, r, "uploads.shadowing.get")
	if !ok {
		return
	}

	var owned bool
	if err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM recordings
			WHERE user_id = $1 AND shadowing_audio_url = $2
		)`, user.ID, *publicURL).Scan(&owned); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load pronunciation audio."})
		return
	}
	if !owned {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	absolutePath, err := storedUploadPath(*publicURL)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeFile(w, r, absolutePath)
}

func storedUploadPath(publicURL string) (string, error) {
	return storage.NewLegacyUploads(resolveUploadsDir()).Path(publicURL)
}

func removeStoredUploadFiles(publicURLs []string) error {
	return storage.NewLegacyUploads(resolveUploadsDir()).Remove(publicURLs)
}
