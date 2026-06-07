package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	return http.StripPrefix(uploadsURLPrefix, http.FileServer(http.Dir(resolveUploadsDir())))
}
