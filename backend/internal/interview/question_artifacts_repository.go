package interview

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Callers hold the session row lock while allocating immutable question indexes.
func createQuestionArtifact(ctx context.Context, tx pgx.Tx, sessionID, question string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM interview_question_artifacts WHERE session_id=$1 AND question=$2`, sessionID, question).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	id, jobID := uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO interview_question_artifacts(id,session_id,question_index,question,job_id)
 VALUES($1,$2,(SELECT COALESCE(MAX(question_index),0)+1 FROM interview_question_artifacts WHERE session_id=$2),$3,$4)`, id, sessionID, question, jobID)
	if err != nil {
		return "", err
	}
	err = workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: jobID, Kind: JobKind, ResourceID: id, IdempotencyKey: "interview.question:" + id, Payload: map[string]string{"step": "question_audio", "sessionId": sessionID}, Priority: 8, MaxAttempts: 4})
	return id, err
}

func (r *SQLRepository) QuestionArtifact(ctx context.Context, input QuestionAudioInput, create bool) (QuestionArtifact, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return QuestionArtifact{}, err
	}
	defer tx.Rollback(ctx)
	var sessionID, status, question, artifactID string
	var index int
	if input.RecordingID != "" {
		err = tx.QueryRow(ctx, `SELECT s.id,s.status,t.question,COALESCE(t.question_artifact_id,'')
   FROM interview_sessions s JOIN recordings r ON r.id=s.recording_id JOIN interview_turns t ON t.session_id=s.id
   WHERE r.id=$1 AND r.user_id=$2 AND s.owner_principal_id=$2 AND t.seq=$3 AND NOT t.skipped FOR UPDATE OF s`, input.RecordingID, input.OwnerID, input.TurnSeq).Scan(&sessionID, &status, &question, &artifactID)
	} else {
		err = tx.QueryRow(ctx, `SELECT id,status FROM interview_sessions WHERE id=$1 AND owner_principal_id=$2
   AND status IN ('preparing','ready','recording') AND expires_at>NOW() FOR UPDATE`, input.SessionID, input.OwnerID).Scan(&sessionID, &status)
		if err == nil {
			err = tx.QueryRow(ctx, `SELECT id,question,question_index FROM interview_question_artifacts WHERE session_id=$1 AND question_index=$2`, sessionID, input.Index).Scan(&artifactID, &question, &index)
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return QuestionArtifact{}, ErrNotFound
	}
	if err != nil {
		return QuestionArtifact{}, err
	}
	if artifactID == "" {
		if !create {
			return QuestionArtifact{SessionID: sessionID, OwnerID: input.OwnerID, Question: question, Status: "not_requested"}, nil
		}
		artifactID, err = createQuestionArtifact(ctx, tx, sessionID, question)
		if err != nil {
			return QuestionArtifact{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE interview_turns SET question_artifact_id=$3 WHERE session_id=$1 AND seq=$2`, sessionID, input.TurnSeq, artifactID); err != nil {
			return QuestionArtifact{}, err
		}
	}
	artifact := QuestionArtifact{ID: artifactID, SessionID: sessionID, OwnerID: input.OwnerID}
	err = tx.QueryRow(ctx, `SELECT question_index,question,status,COALESCE(audio_asset_id,''),COALESCE(error_message,'') FROM interview_question_artifacts WHERE id=$1`, artifactID).Scan(&artifact.Index, &artifact.Question, &artifact.Status, &artifact.AssetID, &artifact.Error)
	if err != nil {
		return QuestionArtifact{}, err
	}
	return artifact, tx.Commit(ctx)
}

func (r *SQLRepository) RetryQuestionAudio(ctx context.Context, input QuestionAudioInput, artifactID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sessionID string
	err = tx.QueryRow(ctx, `SELECT s.id FROM interview_sessions s JOIN interview_question_artifacts q ON q.session_id=s.id
 WHERE q.id=$1 AND s.owner_principal_id=$2 AND
 ((s.status IN ('preparing','ready','recording') AND s.expires_at>NOW()) OR s.recording_id IS NOT NULL) FOR UPDATE OF s`, artifactID, input.OwnerID).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	jobID := uuid.NewString()
	result, err := tx.Exec(ctx, `UPDATE interview_question_artifacts SET status='processing',job_id=$2,error_message=NULL WHERE id=$1 AND status='failed'`, artifactID, jobID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: jobID, Kind: JobKind, ResourceID: artifactID, IdempotencyKey: "interview.question:" + jobID, Payload: map[string]string{"step": "question_audio", "sessionId": sessionID}, Priority: 8, MaxAttempts: 4}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *SQLRepository) LoadQuestionAudio(ctx context.Context, job workqueue.Job) (QuestionArtifact, bool, error) {
	artifact := QuestionArtifact{}
	err := r.db.QueryRow(ctx, `SELECT q.id,q.session_id,s.owner_principal_id,q.question,q.question_index
 FROM interview_question_artifacts q JOIN interview_sessions s ON s.id=q.session_id
 WHERE q.id=$1 AND q.job_id=$2 AND q.status='processing' AND s.status NOT IN ('cancelled','failed')
 AND (s.expires_at>NOW() OR s.recording_id IS NOT NULL OR s.guest_preview_id IS NOT NULL)`, job.ResourceID, job.ID).Scan(&artifact.ID, &artifact.SessionID, &artifact.OwnerID, &artifact.Question, &artifact.Index)
	if errors.Is(err, pgx.ErrNoRows) {
		return artifact, false, nil
	}
	return artifact, err == nil, err
}
func (r *SQLRepository) CompleteQuestionAudio(ctx context.Context, job workqueue.Job, audio, manifest media.Asset) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireInterviewLease(ctx, tx, job); err != nil {
		return err
	}
	var owner string
	err = tx.QueryRow(ctx, `SELECT s.owner_principal_id FROM interview_question_artifacts q JOIN interview_sessions s ON s.id=q.session_id
 WHERE q.id=$1 AND q.job_id=$2 AND q.status='processing' AND s.status NOT IN ('cancelled','failed')
 AND (s.expires_at>NOW() OR s.recording_id IS NOT NULL OR s.guest_preview_id IS NOT NULL) FOR UPDATE OF s,q`, job.ResourceID, job.ID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return workqueue.ErrLeaseLost
	}
	if err != nil {
		return err
	}
	// Promotion may change the owner while synthesis is running. The fenced
	// session determines who owns these server-created question artifacts.
	for _, asset := range []media.Asset{audio, manifest} {
		if _, err := tx.Exec(ctx, `UPDATE media_assets SET owner_principal_id=$2 WHERE id=$1 AND state='ready'`, asset.ID, owner); err != nil {
			return err
		}
		asset.OwnerPrincipalID = owner
		if err := media.AttachGenerated(ctx, tx, asset); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE interview_question_artifacts SET status='ready',audio_asset_id=$2,manifest_asset_id=$3,error_message=NULL WHERE id=$1`, job.ResourceID, audio.ID, manifest.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
