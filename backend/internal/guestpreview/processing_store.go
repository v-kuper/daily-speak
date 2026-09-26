package guestpreview

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/recording"
	"github.com/jackc/pgx/v5"
)

func (s *Store) Claim(ctx context.Context, job Job) (ProcessingWork, bool, error) {
	var state string
	var work ProcessingWork
	err := s.db.QueryRow(ctx, `
		SELECT state, audio_asset_id, transcript, duration FROM guest_previews
		WHERE id = $1 AND preview_job_id = $2`, job.ResourceID, job.ID).
		Scan(&state, &work.AudioAssetID, &work.Transcript, &work.DeclaredDuration)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProcessingWork{}, false, nil
	}
	if err != nil {
		return ProcessingWork{}, false, err
	}
	if state != "queued" && state != "processing" {
		return ProcessingWork{}, false, nil
	}
	if state == "processing" {
		return work, true, nil
	}
	result, err := s.db.Exec(ctx, `
		UPDATE guest_previews SET state = 'processing', processing_error = NULL, updated_at = NOW()
		WHERE id = $1 AND preview_job_id = $2 AND state = 'queued' AND expires_at > NOW()`, job.ResourceID, job.ID)
	if err != nil {
		return ProcessingWork{}, false, err
	}
	return work, result.RowsAffected() > 0, nil
}

func (s *Store) SaveTranscript(ctx context.Context, job Job, transcript string, duration int) (bool, error) {
	result, err := s.db.Exec(ctx, `
		UPDATE guest_previews SET transcript = $3, duration = $4, updated_at = NOW()
		WHERE id = $1 AND preview_job_id = $2 AND state = 'processing' AND expires_at > NOW()
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $2 AND state = 'running' AND lease_token = $5)`,
		job.ResourceID, job.ID, transcript, duration, job.LeaseToken)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (s *Store) UpdateDuration(ctx context.Context, job Job, duration int) error {
	_, err := s.db.Exec(ctx, `
		UPDATE guest_previews SET duration = $3, updated_at = NOW()
		WHERE id = $1 AND preview_job_id = $2 AND state = 'processing' AND expires_at > NOW()
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $2 AND state = 'running' AND lease_token = $4)`,
		job.ResourceID, job.ID, duration, job.LeaseToken)
	return err
}

func (s *Store) IsActive(ctx context.Context, job Job) (bool, error) {
	var active bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM guest_previews
		WHERE id = $1 AND preview_job_id = $2 AND state = 'processing' AND expires_at > NOW())`,
		job.ResourceID, job.ID).Scan(&active)
	return active, err
}

func (s *Store) Complete(ctx context.Context, job Job, corrections []recording.Suggestion) error {
	_, err := s.db.Exec(ctx, `
		UPDATE guest_previews
		SET state = 'ready', preview_corrections = $3::jsonb, processing_error = NULL, updated_at = NOW()
		WHERE id = $1 AND preview_job_id = $2 AND state = 'processing' AND expires_at > NOW()
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $2 AND state = 'running' AND lease_token = $4)`,
		job.ResourceID, job.ID, encodeCorrections(corrections), job.LeaseToken)
	return err
}
