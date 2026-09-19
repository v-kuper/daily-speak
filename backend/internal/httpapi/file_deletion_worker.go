package httpapi

import (
	"context"
	"time"

	"daily-speaking-practice/backend/internal/logging"
)

const fileDeletionInterval = 30 * time.Second

func (s *Server) StartBackgroundWorkers(ctx context.Context) {
	if s.db == nil {
		return
	}
	s.fileDeletionWorkerOnce.Do(func() {
		go s.runFileDeletionWorker(ctx)
	})
}

func (s *Server) wakeFileDeletionWorker() {
	select {
	case s.fileDeletionWake <- struct{}{}:
	default:
	}
}

func (s *Server) runFileDeletionWorker(ctx context.Context) {
	logger := logging.ForBackground("recording.file_cleanup")
	ticker := time.NewTicker(fileDeletionInterval)
	defer ticker.Stop()

	s.processPendingFileDeletions(ctx, logger)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.fileDeletionWake:
			s.processPendingFileDeletions(ctx, logger)
		case <-ticker.C:
			s.processPendingFileDeletions(ctx, logger)
		}
	}
}

func (s *Server) processPendingFileDeletions(ctx context.Context, logger logging.Logger) {
	rows, err := s.db.Query(ctx, `
		SELECT public_url
		FROM pending_file_deletions
		ORDER BY updated_at ASC
		LIMIT 100`)
	if err != nil {
		if ctx.Err() == nil {
			logger.Error("file_cleanup.load_failed", logging.ErrorMeta(err))
		}
		return
	}
	fileURLs := []string{}
	for rows.Next() {
		var fileURL string
		if err := rows.Scan(&fileURL); err != nil {
			rows.Close()
			logger.Error("file_cleanup.scan_failed", logging.ErrorMeta(err))
			return
		}
		fileURLs = append(fileURLs, fileURL)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		logger.Error("file_cleanup.load_failed", logging.ErrorMeta(rowsErr))
		return
	}

	for _, fileURL := range fileURLs {
		if ctx.Err() != nil {
			return
		}
		if cleanupErr := s.removeStoredUploads([]string{fileURL}); cleanupErr != nil {
			_, updateErr := s.db.Exec(ctx, `
				UPDATE pending_file_deletions
				SET attempts = attempts + 1, last_error = $2, updated_at = NOW()
				WHERE public_url = $1`, fileURL, cleanupErr.Error())
			meta := map[string]any{"publicUrl": fileURL, "errorMessage": cleanupErr.Error()}
			if updateErr != nil {
				meta["queueUpdateError"] = updateErr.Error()
			}
			logger.Warn("file_cleanup.retry_scheduled", meta)
			continue
		}

		if _, err := s.db.Exec(ctx, `DELETE FROM pending_file_deletions WHERE public_url = $1`, fileURL); err != nil {
			logger.Warn("file_cleanup.dequeue_failed", map[string]any{"publicUrl": fileURL, "errorMessage": err.Error()})
		}
	}
}
