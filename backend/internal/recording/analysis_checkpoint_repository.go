package recording

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"daily-speaking-practice/backend/internal/workqueue"
)

type AnalysisCheckpoint interface {
	Load(context.Context, SuggestionCategory) ([]analysisCandidate, bool, error)
	Save(context.Context, SuggestionCategory, []analysisCandidate) error
}

type AnalysisCheckpointRepository interface {
	AnalysisCheckpoint(ProcessingJob, AnalysisInput) AnalysisCheckpoint
}

type sqlAnalysisCheckpoint struct {
	repository  *SQLProcessingRepository
	job         ProcessingJob
	fingerprint string
}

func (r *SQLProcessingRepository) AnalysisCheckpoint(job ProcessingJob, input AnalysisInput) AnalysisCheckpoint {
	input.Checkpoint = nil
	data, _ := json.Marshal(input)
	hash := sha256.Sum256(append([]byte("feedback-v2:"), data...))
	return &sqlAnalysisCheckpoint{repository: r, job: job, fingerprint: fmt.Sprintf("%x", hash)}
}

func (c *sqlAnalysisCheckpoint) Load(ctx context.Context, category SuggestionCategory) ([]analysisCandidate, bool, error) {
	var payload []byte
	err := c.repository.db.QueryRow(ctx, `SELECT COALESCE(analysis_checkpoints -> $2::text, 'null'::jsonb)
		FROM recordings WHERE id = $1`, c.job.ResourceID, string(category)).Scan(&payload)
	if err != nil {
		return nil, false, err
	}
	var stored struct {
		Fingerprint string              `json:"fingerprint"`
		Candidates  []analysisCandidate `json:"candidates"`
	}
	if json.Unmarshal(payload, &stored) != nil || stored.Fingerprint != c.fingerprint {
		return nil, false, nil
	}
	return stored.Candidates, true, nil
}

func (c *sqlAnalysisCheckpoint) Save(ctx context.Context, category SuggestionCategory, candidates []analysisCandidate) error {
	payload, err := json.Marshal(struct {
		Fingerprint string              `json:"fingerprint"`
		Candidates  []analysisCandidate `json:"candidates"`
	}{c.fingerprint, candidates})
	if err != nil {
		return err
	}
	tx, err := c.repository.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireRecordingLease(ctx, tx, c.job); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE recordings SET analysis_checkpoints =
		analysis_checkpoints || jsonb_build_object($2::text, $3::jsonb)
		WHERE id = $1 AND status = 'processing' AND processing_job_id = $4
		AND EXISTS (SELECT 1 FROM processing_jobs WHERE id = $4 AND state = 'running' AND lease_token = $5 AND lease_expires_at > NOW())`,
		c.job.ResourceID, string(category), string(payload), c.job.ID, c.job.LeaseToken)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return workqueue.ErrLeaseLost
	}
	return tx.Commit(ctx)
}
