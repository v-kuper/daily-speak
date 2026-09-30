package recording

import (
	"context"
	"errors"
	"strings"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SQLFeedbackAudioRepository struct{ db *db.DB }

func NewSQLFeedbackAudioRepository(database *db.DB) *SQLFeedbackAudioRepository {
	return &SQLFeedbackAudioRepository{database}
}

type feedbackQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findFocus(ctx context.Context, q feedbackQuerier, input FeedbackAudioInput) (FeedbackFocus, error) {
	var payload []byte
	var transcript string
	var sequence int
	var err error
	answers := map[int]string{}
	if input.AttemptID == "" {
		err = q.QueryRow(ctx, `SELECT focused_feedback,transcript FROM recordings WHERE id=$1 AND user_id=$2`, input.RecordingID, input.OwnerID).Scan(&payload, &transcript)
		answers[0] = transcript
	} else {
		err = q.QueryRow(ctx, `SELECT a.focused_feedback,a.transcript,a.turn_seq FROM interview_answer_attempts a JOIN recordings r ON r.id=a.recording_id
   WHERE a.id=$1 AND r.id=$2 AND r.user_id=$3 AND a.status='ready'`, input.AttemptID, input.RecordingID, input.OwnerID).Scan(&payload, &transcript, &sequence)
		answers[sequence] = transcript
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return FeedbackFocus{}, ErrNotFound
	}
	if err != nil {
		return FeedbackFocus{}, err
	}
	feedback := DecodeFocusedFeedback(payload)
	if feedback != nil {
		for _, answer := range feedback.Answers {
			for _, item := range answer.Items {
				if item.ID == input.FeedbackID {
					return item, nil
				}
				if !strings.HasPrefix(input.FeedbackID, "context-v1-") || item.Span == nil || item.Span.TurnSequence != answer.TurnSequence {
					continue
				}
				// Legacy example audio remains addressable by its original focus ID.
				// Context audio uses a separate content-derived ID and its own cache.
				if _, loaded := answers[answer.TurnSequence]; !loaded && input.AttemptID == "" && answer.TurnSequence > 0 {
					err := q.QueryRow(ctx, `SELECT COALESCE(NULLIF(t.final_transcript,''),t.provisional_transcript,'')
 FROM interview_turns t JOIN interview_sessions s ON s.id=t.session_id
 WHERE s.recording_id=$1 AND t.seq=$2 AND NOT t.skipped`, input.RecordingID, answer.TurnSequence).Scan(&transcript)
					if err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							continue
						}
						return FeedbackFocus{}, err
					}
					answers[answer.TurnSequence] = transcript
				}
				context := feedbackPracticeContext(answers[answer.TurnSequence], item)
				if context != nil && context.AudioFeedbackID == input.FeedbackID {
					item.ID, item.PracticeText, item.PracticeContext = context.AudioFeedbackID, context.CorrectedText, context
					return item, nil
				}
			}
		}
	}
	return FeedbackFocus{}, ErrNotFound
}
func (r *SQLFeedbackAudioRepository) FindFocus(ctx context.Context, input FeedbackAudioInput) (FeedbackFocus, error) {
	return findFocus(ctx, r.db, input)
}
func (r *SQLFeedbackAudioRepository) AudioState(ctx context.Context, input FeedbackAudioInput) (FeedbackAudioState, error) {
	state := FeedbackAudioState{}
	err := r.db.QueryRow(ctx, `SELECT status,COALESCE(error_message,''),COALESCE(audio_asset_id,'') FROM recording_feedback_audio
 WHERE recording_id=$1 AND COALESCE(attempt_id,'')=$2 AND feedback_id=$3 AND owner_principal_id=$4`, input.RecordingID, input.AttemptID, input.FeedbackID, input.OwnerID).Scan(&state.Status, &state.Error, &state.AssetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return FeedbackAudioState{Status: "not_requested"}, nil
	}
	return state, err
}
func (r *SQLFeedbackAudioRepository) ScheduleAudio(ctx context.Context, input FeedbackAudioInput, practiceText string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `SELECT id FROM recordings WHERE id=$1 AND user_id=$2 FOR UPDATE`, input.RecordingID, input.OwnerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	focus, err := findFocus(ctx, tx, input)
	if err != nil {
		return err
	}
	if focus.PracticeText != practiceText || practiceText == "" {
		return ErrFeedbackUnavailable
	}
	jobID := uuid.NewString()
	audioID := uuid.NewString()
	err = tx.QueryRow(ctx, `INSERT INTO recording_feedback_audio(id,recording_id,attempt_id,feedback_id,practice_text,owner_principal_id,job_id)
 VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7)
 ON CONFLICT(recording_id,(COALESCE(attempt_id,'')),feedback_id) DO UPDATE
 SET status='processing',job_id=EXCLUDED.job_id,error_message=NULL
 WHERE recording_feedback_audio.status='failed'
 RETURNING id`, audioID, input.RecordingID, input.AttemptID, input.FeedbackID, practiceText, input.OwnerID, jobID).Scan(&audioID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{ID: jobID, Kind: workqueue.KindRecordingFeedbackAudio, ResourceID: audioID, IdempotencyKey: "feedback.audio:" + jobID, MaxAttempts: 4}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *SQLFeedbackAudioRepository) LoadAudioWork(ctx context.Context, job workqueue.Job) (FeedbackAudioWork, bool, error) {
	work := FeedbackAudioWork{}
	err := r.db.QueryRow(ctx, `SELECT a.id,a.owner_principal_id,a.practice_text,a.recording_id
 FROM recording_feedback_audio a JOIN recordings r ON r.id=a.recording_id
 WHERE a.id=$1 AND a.job_id=$2 AND a.status='processing' AND a.owner_principal_id=r.user_id`, job.ResourceID, job.ID).Scan(&work.ID, &work.OwnerID, &work.PracticeText, &work.RecordingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return work, false, nil
	}
	return work, err == nil, err
}
func (r *SQLFeedbackAudioRepository) CompleteAudio(ctx context.Context, job workqueue.Job, asset media.Asset) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireRecordingLease(ctx, tx, ProcessingJob{ID: job.ID, LeaseToken: job.LeaseToken}); err != nil {
		return err
	}
	var owner string
	err = tx.QueryRow(ctx, `SELECT owner_principal_id FROM recording_feedback_audio WHERE id=$1 AND job_id=$2 AND status='processing' FOR UPDATE`, job.ResourceID, job.ID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return workqueue.ErrLeaseLost
	}
	if err != nil {
		return err
	}
	if owner != asset.OwnerPrincipalID {
		return ErrNotFound
	}
	if err := media.AttachGenerated(ctx, tx, asset); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE recording_feedback_audio SET status='ready',audio_asset_id=$2,error_message=NULL WHERE id=$1`, job.ResourceID, asset.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *SQLFeedbackAudioRepository) FinalizeAudioFailure(ctx context.Context, tx pgx.Tx, job workqueue.Job) error {
	_, err := tx.Exec(ctx, `UPDATE recording_feedback_audio SET status='failed',error_message='Не удалось подготовить аудио. Попробуйте ещё раз.' WHERE id=$1 AND job_id=$2 AND status='processing'`, job.ResourceID, job.ID)
	return err
}
