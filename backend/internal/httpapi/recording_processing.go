package httpapi

import (
	"context"
	"time"

	"daily-speaking-practice/backend/internal/logging"
	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/workqueue"
)

const recordingProcessingTimeout = 30 * time.Minute

func (s *Server) runRecordingJob(ctx context.Context, job workqueue.Job) error {
	return s.recordingProcessor.Process(ctx, recording.ProcessingJob{
		ID: job.ID, ResourceID: job.ResourceID, LeaseToken: job.LeaseToken,
	}, logging.ForBackground("worker.recordings.process"))
}
