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
		SELECT p.state, p.audio_asset_id, p.transcript, p.duration,
		       (SELECT session.id FROM interview_sessions session WHERE session.guest_preview_id = p.id)
		FROM guest_previews p
		WHERE p.id = $1 AND p.preview_job_id = $2`, job.ResourceID, job.ID).
		Scan(&state, &work.AudioAssetID, &work.Transcript, &work.DeclaredDuration, &work.InterviewSessionID)
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

func (s *Store) LoadInterviewTurns(ctx context.Context, sessionID string) ([]recording.InterviewTurn, error) {
	rows, err := s.db.Query(ctx, `
		SELECT seq, question, asked_at_ms, ended_at_ms, transcript_status,
		       provisional_transcript, final_transcript
		FROM interview_turns WHERE session_id = $1 ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var turns []recording.InterviewTurn
	for rows.Next() {
		var turn recording.InterviewTurn
		if err := rows.Scan(&turn.Sequence, &turn.Question, &turn.AskedAtMS, &turn.EndedAtMS,
			&turn.TranscriptStatus, &turn.Provisional, &turn.FinalText); err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

func (s *Store) SealInterviewLastTurn(ctx context.Context, job Job, sessionID string, actualMS int) error {
	_, err := s.db.Exec(ctx, `
		UPDATE interview_turns t
		SET ended_at_ms = GREATEST(t.asked_at_ms + 1, $4), updated_at = NOW()
		FROM interview_sessions session
		WHERE t.session_id = session.id AND session.id = $1 AND session.guest_preview_id = $2
		  AND t.seq = (SELECT MAX(seq) FROM interview_turns WHERE session_id = $1)
		  AND EXISTS (SELECT 1 FROM guest_previews p
		              WHERE p.id = $2 AND p.preview_job_id = $3 AND p.state = 'processing')
		  AND EXISTS (SELECT 1 FROM processing_jobs j
		              WHERE j.id = $3 AND j.state = 'running' AND j.lease_token = $5)`,
		sessionID, job.ResourceID, job.ID, actualMS, job.LeaseToken)
	return err
}

func (s *Store) SaveInterviewTranscript(ctx context.Context, job Job, transcript string, duration int, sessionID string, answers map[int]string) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `
		UPDATE guest_previews SET transcript = $3, duration = $4, updated_at = NOW()
		WHERE id = $1 AND preview_job_id = $2 AND state = 'processing' AND expires_at > NOW()
		  AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $2 AND state = 'running' AND lease_token = $5)`,
		job.ResourceID, job.ID, transcript, duration, job.LeaseToken)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 0 {
		return false, nil
	}
	for sequence, answer := range answers {
		result, err := tx.Exec(ctx, `
			UPDATE interview_turns t
			SET final_transcript = $3, updated_at = NOW()
			FROM interview_sessions session
			WHERE t.session_id = session.id AND session.id = $1 AND session.guest_preview_id = $2
			  AND t.seq = $4`, sessionID, job.ResourceID, answer, sequence)
		if err != nil {
			return false, err
		}
		if result.RowsAffected() != 1 {
			return false, errors.New("interview timeline changed during guest preview processing")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
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
