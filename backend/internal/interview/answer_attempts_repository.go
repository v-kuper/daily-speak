package interview

import (
	"context"
	"encoding/json"
	"errors"

	"daily-speaking-practice/backend/internal/recording"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *SQLRepository) OriginalAnswer(ctx context.Context, owner, recordingID string, seq int) (AttemptOriginal, error) {
	original := AttemptOriginal{}
	err := r.db.QueryRow(ctx, `SELECT s.id,r.topic,t.question,s.english_level FROM recordings r
 JOIN interview_sessions s ON s.recording_id=r.id JOIN interview_turns t ON t.session_id=s.id
 WHERE r.id=$1 AND r.user_id=$2 AND s.owner_principal_id=$2 AND r.transcript<>'' AND t.seq=$3 AND NOT t.skipped`, recordingID, owner, seq).Scan(&original.SessionID, &original.Topic, &original.Question, &original.EnglishLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return original, ErrNotFound
	}
	if err != nil {
		return original, err
	}
	rows, err := r.db.Query(ctx, `SELECT seq,question,COALESCE(final_transcript,provisional_transcript,'') FROM interview_turns WHERE session_id=$1 AND NOT skipped ORDER BY seq`, original.SessionID)
	if err != nil {
		return original, err
	}
	defer rows.Close()
	for rows.Next() {
		var t recording.InterviewDialogueTurn
		if err := rows.Scan(&t.Sequence, &t.Question, &t.Answer); err != nil {
			return original, err
		}
		original.Dialogue = append(original.Dialogue, t)
	}
	return original, rows.Err()
}

const attemptColumns = `id,recording_id,turn_seq,status,transcript,duration_seconds,focused_feedback,COALESCE(error_message,''),created_at,audio_asset_id`

func scanAttempt(row pgx.Row) (AnswerAttempt, error) {
	attempt := AnswerAttempt{}
	var feedback []byte
	err := row.Scan(&attempt.ID, &attempt.RecordingID, &attempt.TurnSequence, &attempt.Status, &attempt.Transcript, &attempt.DurationSeconds, &feedback, &attempt.Error, &attempt.CreatedAt, &attempt.AudioAssetID)
	attempt.Feedback = recording.DecodeFocusedFeedbackForAnswers(feedback, map[int]string{attempt.TurnSequence: attempt.Transcript})
	return attempt, err
}
func (r *SQLRepository) CreateAttempt(ctx context.Context, input CreateAttemptInput) (AnswerAttempt, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return AnswerAttempt{}, err
	}
	defer tx.Rollback(ctx)
	var sessionID string
	err = tx.QueryRow(ctx, `SELECT s.id FROM recordings r JOIN interview_sessions s ON s.recording_id=r.id
 JOIN interview_turns t ON t.session_id=s.id WHERE r.id=$1 AND r.user_id=$2 AND s.owner_principal_id=$2
 AND r.status='ready' AND t.seq=$3 AND NOT t.skipped FOR UPDATE OF r,s`, input.RecordingID, input.OwnerID, input.TurnSeq).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnswerAttempt{}, ErrNotFound
	}
	if err != nil {
		return AnswerAttempt{}, err
	}
	existing, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM interview_answer_attempts WHERE recording_id=$1 AND create_key=$2`, input.RecordingID, input.IdempotencyKey))
	if err == nil {
		if existing.AudioAssetID != input.AudioAssetID || existing.TurnSequence != input.TurnSeq {
			return AnswerAttempt{}, ErrConflict
		}
		if existing.Status == "failed" {
			jobID := uuid.NewString()
			if _, err := tx.Exec(ctx, `UPDATE interview_answer_attempts SET status='processing',job_id=$2,error_message=NULL WHERE id=$1`, existing.ID, jobID); err != nil {
				return AnswerAttempt{}, err
			}
			if err := enqueueAttempt(ctx, tx, jobID, existing.ID, sessionID); err != nil {
				return AnswerAttempt{}, err
			}
			existing.Status = "processing"
			existing.Error = ""
		}
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AnswerAttempt{}, err
	}
	var assetID string
	err = tx.QueryRow(ctx, `SELECT id FROM media_assets WHERE id=$1 AND owner_principal_id=$2 AND purpose='interview_attempt_audio'
 AND state='ready' AND attached_at IS NULL AND deleted_at IS NULL FOR UPDATE`, input.AudioAssetID, input.OwnerID).Scan(&assetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnswerAttempt{}, ErrNotFound
	}
	if err != nil {
		return AnswerAttempt{}, err
	}
	id, jobID := uuid.NewString(), uuid.NewString()
	attempt, err := scanAttempt(tx.QueryRow(ctx, `INSERT INTO interview_answer_attempts(id,recording_id,session_id,turn_seq,owner_principal_id,create_key,audio_asset_id,job_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+attemptColumns, id, input.RecordingID, sessionID, input.TurnSeq, input.OwnerID, input.IdempotencyKey, input.AudioAssetID, jobID))
	if err != nil {
		return AnswerAttempt{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE media_assets SET attached_at=NOW(),retention_until=NULL WHERE id=$1`, assetID); err != nil {
		return AnswerAttempt{}, err
	}
	if err := enqueueAttempt(ctx, tx, jobID, id, sessionID); err != nil {
		return AnswerAttempt{}, err
	}
	return attempt, tx.Commit(ctx)
}
func enqueueAttempt(ctx context.Context, tx pgx.Tx, jobID, attemptID, sessionID string) error {
	return workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: jobID, Kind: JobKind, ResourceID: attemptID, IdempotencyKey: "interview.attempt:" + jobID, Payload: map[string]string{"step": "answer_attempt", "sessionId": sessionID}, Priority: 4, MaxAttempts: 3})
}
func (r *SQLRepository) GetAttempt(ctx context.Context, owner, recordingID, id string) (AnswerAttempt, error) {
	attempt, err := scanAttempt(r.db.QueryRow(ctx, `SELECT `+attemptColumns+` FROM interview_answer_attempts
 WHERE id=$1 AND recording_id=$2 AND owner_principal_id=$3 AND EXISTS(SELECT 1 FROM recordings WHERE id=$2 AND user_id=$3)`, id, recordingID, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return attempt, ErrNotFound
	}
	return attempt, err
}
func (r *SQLRepository) ListAttempts(ctx context.Context, owner, recordingID string, seq, limit int, beforeID string) ([]AnswerAttempt, error) {
	rows, err := r.db.Query(ctx, `SELECT `+attemptColumns+` FROM interview_answer_attempts
 WHERE recording_id=$1 AND owner_principal_id=$2 AND turn_seq=$3
 AND ($4::text='' OR (created_at,id)<(SELECT created_at,id FROM interview_answer_attempts WHERE id=$4 AND recording_id=$1 AND turn_seq=$3))
 ORDER BY created_at DESC,id DESC LIMIT $5`, recordingID, owner, seq, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attempts := []AnswerAttempt{}
	for rows.Next() {
		a := AnswerAttempt{}
		var payload []byte
		if err := rows.Scan(&a.ID, &a.RecordingID, &a.TurnSequence, &a.Status, &a.Transcript, &a.DurationSeconds, &payload, &a.Error, &a.CreatedAt, &a.AudioAssetID); err != nil {
			return nil, err
		}
		a.Feedback = recording.DecodeFocusedFeedbackForAnswers(payload, map[int]string{a.TurnSequence: a.Transcript})
		attempts = append(attempts, a)
	}
	return attempts, rows.Err()
}
func (r *SQLRepository) LoadAttemptWork(ctx context.Context, job workqueue.Job) (AnswerAttemptWork, bool, error) {
	work := AnswerAttemptWork{}
	attempt, err := scanAttempt(r.db.QueryRow(ctx, `SELECT `+attemptColumns+` FROM interview_answer_attempts WHERE id=$1 AND job_id=$2 AND status='processing'`, job.ResourceID, job.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return work, false, nil
	}
	if err != nil {
		return work, false, err
	}
	work.Attempt = attempt
	if err := r.db.QueryRow(ctx, `SELECT owner_principal_id FROM interview_answer_attempts WHERE id=$1`, attempt.ID).Scan(&work.OwnerID); err != nil {
		return work, false, err
	}
	work.Original, err = r.OriginalAnswer(ctx, work.OwnerID, attempt.RecordingID, attempt.TurnSequence)
	return work, err == nil, err
}
func (r *SQLRepository) SaveAttemptTranscript(ctx context.Context, job workqueue.Job, transcript string, seconds int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireInterviewLease(ctx, tx, job); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE interview_answer_attempts SET transcript=$3,duration_seconds=$4
 WHERE id=$1 AND job_id=$2 AND status='processing'`, job.ResourceID, job.ID, transcript, seconds)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return workqueue.ErrLeaseLost
	}
	return tx.Commit(ctx)
}
func (r *SQLRepository) CompleteAttempt(ctx context.Context, job workqueue.Job, feedback *recording.FocusedFeedback) error {
	if feedback == nil {
		return ErrInvalid
	}
	payload, err := json.Marshal(feedback)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireInterviewLease(ctx, tx, job); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE interview_answer_attempts SET status='ready',focused_feedback=$3::jsonb,error_message=NULL
 WHERE id=$1 AND job_id=$2 AND status='processing'`, job.ResourceID, job.ID, string(payload))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return workqueue.ErrLeaseLost
	}
	return tx.Commit(ctx)
}

func (r *SQLRepository) SaveAttemptFeedback(ctx context.Context, job workqueue.Job, feedback *recording.FocusedFeedback) error {
	payload, err := json.Marshal(feedback)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireInterviewLease(ctx, tx, job); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE interview_answer_attempts SET focused_feedback=$3::jsonb WHERE id=$1 AND job_id=$2 AND status='processing'`, job.ResourceID, job.ID, string(payload))
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return workqueue.ErrLeaseLost
	}
	return tx.Commit(ctx)
}

func (r *SQLRepository) RetryAttempt(ctx context.Context, owner, recordingID, id string) (AnswerAttempt, error) {
	input := CreateAttemptInput{OwnerID: owner, RecordingID: recordingID}
	err := r.db.QueryRow(ctx, `SELECT audio_asset_id,turn_seq,create_key FROM interview_answer_attempts WHERE id=$1 AND recording_id=$2 AND owner_principal_id=$3`, id, recordingID, owner).Scan(&input.AudioAssetID, &input.TurnSeq, &input.IdempotencyKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnswerAttempt{}, ErrNotFound
	}
	if err != nil {
		return AnswerAttempt{}, err
	}
	return r.CreateAttempt(ctx, input)
}
