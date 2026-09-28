package interview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type SQLRepository struct{ db *db.DB }

func NewSQLRepository(database *db.DB) *SQLRepository { return &SQLRepository{db: database} }

func (r *SQLRepository) FindByCreateKey(ctx context.Context, ownerPrincipalID, key, digest string) (Session, bool, error) {
	var id, storedDigest string
	err := r.db.QueryRow(ctx, `SELECT id,request_digest FROM interview_sessions
		WHERE owner_principal_id=$1 AND create_key=$2`, ownerPrincipalID, key).Scan(&id, &storedDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	if storedDigest != digest {
		return Session{}, false, ErrConflict
	}
	session, err := r.Get(ctx, ownerPrincipalID, id)
	return session, err == nil, err
}

func (r *SQLRepository) Create(ctx context.Context, input CreateInput, maxSeconds int) (Session, error) {
	if r == nil || r.db == nil || maxSeconds <= 0 || maxSeconds > 600 {
		return Session{}, ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	var previousID, previousDigest string
	err = tx.QueryRow(ctx, `SELECT id,request_digest FROM interview_sessions
		WHERE owner_principal_id=$1 AND create_key=$2`, input.OwnerPrincipalID, input.IdempotencyKey).Scan(&previousID, &previousDigest)
	if err == nil {
		if previousDigest != input.RequestDigest {
			return Session{}, ErrConflict
		}
		return r.Get(ctx, input.OwnerPrincipalID, previousID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, err
	}
	// An expired preparation must not block a new interview forever.
	_, err = tx.Exec(ctx, `UPDATE interview_sessions SET status = 'failed', error_message = 'Session expired', updated_at = NOW()
		WHERE owner_principal_id = $1 AND status IN ('preparing','ready','recording') AND expires_at <= NOW()`, input.OwnerPrincipalID)
	if err != nil {
		return Session{}, err
	}
	if input.UserID == "" {
		var hasPreview bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM guest_previews WHERE guest_principal_id = $1 AND state <> 'expired')`, input.OwnerPrincipalID).Scan(&hasPreview)
		if err != nil {
			return Session{}, err
		}
		if hasPreview {
			return Session{}, ErrQuota
		}
	}
	interestsJSON, _ := json.Marshal(input.Interests)
	id := uuid.NewString()
	var nullableUser any
	if input.UserID != "" {
		nullableUser = input.UserID
	}
	_, err = tx.Exec(ctx, `INSERT INTO interview_sessions
		(id, owner_principal_id, create_key, request_digest, user_id, topic, opening_question, english_level, interests, status, max_duration_seconds)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,'preparing',$10)`,
		id, input.OwnerPrincipalID, input.IdempotencyKey, input.RequestDigest, nullableUser,
		input.Topic, input.OpeningQuestion, input.EnglishLevel, string(interestsJSON), maxSeconds)
	if err != nil {
		if uniqueViolation(err) {
			_ = tx.Rollback(ctx)
			if existing, found, findErr := r.FindByCreateKey(ctx, input.OwnerPrincipalID, input.IdempotencyKey, input.RequestDigest); findErr != nil {
				return Session{}, findErr
			} else if found {
				return existing, nil
			}
			return Session{}, ErrConflict
		}
		return Session{}, err
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
		ID: uuid.NewString(), Kind: JobKind, ResourceID: id, IdempotencyKey: "interview.prepare:" + id,
		Payload: map[string]string{"step": "prepare"}, Priority: 5, MaxAttempts: 3,
	}); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return r.Get(ctx, input.OwnerPrincipalID, id)
}

func (r *SQLRepository) Cancel(ctx context.Context, ownerPrincipalID, sessionID string) (Session, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	row, err := r.sessionRow(ctx, tx, ownerPrincipalID, sessionID, true)
	if err != nil {
		return Session{}, err
	}
	if row.Status == StatusCancelled {
		return r.Get(ctx, ownerPrincipalID, sessionID)
	}
	if row.Status == StatusFinalized || row.Status == StatusFinalizing ||
		row.RecordingID != "" || row.GuestPreviewID != "" {
		return Session{}, ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE interview_sessions SET status='cancelled',updated_at=NOW() WHERE id=$1`, sessionID)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return r.Get(ctx, ownerPrincipalID, sessionID)
}

func (r *SQLRepository) Get(ctx context.Context, ownerPrincipalID, sessionID string) (Session, error) {
	row, err := r.sessionRow(ctx, r.db, ownerPrincipalID, sessionID, false)
	if err != nil {
		return Session{}, err
	}
	return r.view(ctx, row)
}

// EnsureRefill is invoked by polling clients when a failed reserve job left the
// queue empty. It only enqueues durable work; the request never calls a model.
func (r *SQLRepository) EnsureRefill(ctx context.Context, ownerPrincipalID, sessionID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	row, err := r.sessionRow(ctx, tx, ownerPrincipalID, sessionID, true)
	if err != nil {
		return err
	}
	if row.Status != StatusRecording || row.RecordingID != "" || row.GuestPreviewID != "" {
		return nil
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM interview_candidates WHERE session_id=$1
		AND consumed_by_turn_seq IS NULL`, sessionID).Scan(&remaining); err != nil {
		return err
	}
	if remaining > 1 {
		return nil
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM processing_jobs
		WHERE kind='interview.process' AND resource_id=$1 AND payload->>'step'='refill'
		AND state IN ('queued','running','retry_wait'))`, sessionID).Scan(&active); err != nil {
		return err
	}
	if active {
		return nil
	}
	key := fmt.Sprintf("interview.refill:%s:retry:%d", sessionID, time.Now().UTC().Unix()/60)
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
		ID: uuid.NewString(), Kind: JobKind, ResourceID: sessionID,
		IdempotencyKey: key, Payload: map[string]string{"step": "refill"},
		Priority: 6, MaxAttempts: 3,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SQLRepository) Start(ctx context.Context, ownerPrincipalID, sessionID string) (Session, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	row, err := r.sessionRow(ctx, tx, ownerPrincipalID, sessionID, true)
	if err != nil {
		return Session{}, err
	}
	if row.Status == StatusRecording {
		return r.Get(ctx, ownerPrincipalID, sessionID)
	}
	if row.Status != StatusReady || !row.ExpiresAt.After(time.Now().UTC()) {
		return Session{}, ErrNotReady
	}
	var candidateCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM interview_candidates WHERE session_id=$1 AND consumed_by_turn_seq IS NULL`, sessionID).Scan(&candidateCount); err != nil {
		return Session{}, err
	}
	if candidateCount != 3 {
		return Session{}, ErrNotReady
	}
	_, err = tx.Exec(ctx, `INSERT INTO interview_turns(id,session_id,seq,question,asked_at_ms)
		VALUES($1,$2,1,$3,0)`, uuid.NewString(), sessionID, row.OpeningQuestion)
	if err != nil {
		return Session{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE interview_sessions SET status='recording',started_at=NOW(),updated_at=NOW() WHERE id=$1`, sessionID)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return r.Get(ctx, ownerPrincipalID, sessionID)
}

func (r *SQLRepository) Advance(ctx context.Context, input AdvanceInput) (Session, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	row, err := r.sessionRow(ctx, tx, input.OwnerPrincipalID, input.SessionID, true)
	if err != nil {
		return Session{}, err
	}
	var existingSeq, existingAtMs int
	var existingCandidate string
	err = tx.QueryRow(ctx, `SELECT seq,asked_at_ms,COALESCE(source_candidate_id,'') FROM interview_turns
		WHERE session_id=$1 AND advance_key=$2`, input.SessionID, input.IdempotencyKey).Scan(&existingSeq, &existingAtMs, &existingCandidate)
	if err == nil {
		if existingSeq != input.CurrentTurnSeq+1 || existingAtMs != input.AtMs || existingCandidate != input.NextCandidateID {
			return Session{}, ErrConflict
		}
		return r.Get(ctx, input.OwnerPrincipalID, input.SessionID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, err
	}
	if row.Status != StatusRecording || !row.ExpiresAt.After(time.Now().UTC()) {
		return Session{}, ErrConflict
	}
	if input.AtMs >= row.MaxDurationSeconds*1000 {
		return Session{}, ErrDurationLimit
	}
	if row.RecordingID != "" || row.GuestPreviewID != "" {
		return Session{}, ErrConflict
	}
	var currentSeq, currentAtMs int
	var currentEnded sql.NullInt64
	err = tx.QueryRow(ctx, `SELECT seq,asked_at_ms,ended_at_ms FROM interview_turns
		WHERE session_id=$1 ORDER BY seq DESC LIMIT 1 FOR UPDATE`, input.SessionID).Scan(&currentSeq, &currentAtMs, &currentEnded)
	if err != nil {
		return Session{}, err
	}
	if currentSeq != input.CurrentTurnSeq || currentEnded.Valid || input.AtMs-currentAtMs < 300 {
		return Session{}, ErrConflict
	}
	var question string
	err = tx.QueryRow(ctx, `SELECT question FROM interview_candidates
		WHERE id=$1 AND session_id=$2 AND consumed_by_turn_seq IS NULL FOR UPDATE`,
		input.NextCandidateID, input.SessionID).Scan(&question)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotReady
	}
	if err != nil {
		return Session{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE interview_turns SET ended_at_ms=$3,updated_at=NOW()
		WHERE session_id=$1 AND seq=$2`, input.SessionID, currentSeq, input.AtMs)
	if err != nil {
		return Session{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE interview_candidates SET consumed_by_turn_seq=$3
		WHERE id=$1 AND session_id=$2`, input.NextCandidateID, input.SessionID, currentSeq+1)
	if err != nil {
		return Session{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO interview_turns
		(id,session_id,seq,question,asked_at_ms,advance_key,source_candidate_id)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.NewString(), input.SessionID, currentSeq+1,
		question, input.AtMs, input.IdempotencyKey, input.NextCandidateID)
	if err != nil {
		return Session{}, err
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM interview_candidates WHERE session_id=$1
		AND consumed_by_turn_seq IS NULL`, input.SessionID).Scan(&remaining); err != nil {
		return Session{}, err
	}
	if remaining <= 2 {
		if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
			ID: uuid.NewString(), Kind: JobKind, ResourceID: input.SessionID,
			IdempotencyKey: fmt.Sprintf("interview.refill:%s:%d", input.SessionID, currentSeq+1),
			Payload:        map[string]string{"step": "refill"}, Priority: 6, MaxAttempts: 3,
		}); err != nil {
			return Session{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return r.Get(ctx, input.OwnerPrincipalID, input.SessionID)
}

func (r *SQLRepository) AttachAudio(ctx context.Context, input AttachAudioInput) (Session, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	row, err := r.sessionRow(ctx, tx, input.OwnerPrincipalID, input.SessionID, true)
	if err != nil {
		return Session{}, err
	}
	if row.Status != StatusRecording && row.Status != StatusFinalizing && row.Status != StatusFinalized {
		return Session{}, ErrConflict
	}
	var turnID string
	var endedAt sql.NullInt64
	var existingAsset, existingKey sql.NullString
	err = tx.QueryRow(ctx, `SELECT id,ended_at_ms,audio_asset_id,audio_idempotency_key
		FROM interview_turns WHERE session_id=$1 AND seq=$2 FOR UPDATE`, input.SessionID, input.TurnSeq).
		Scan(&turnID, &endedAt, &existingAsset, &existingKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if existingAsset.Valid {
		if existingAsset.String != input.AudioAssetID || existingKey.String != input.IdempotencyKey {
			return Session{}, ErrConflict
		}
		return r.Get(ctx, input.OwnerPrincipalID, input.SessionID)
	}
	_ = endedAt
	var ready bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_assets a
		JOIN media_uploads u ON u.asset_id=a.id
		WHERE a.id=$1 AND a.owner_principal_id=$2 AND u.interview_session_id=$3
		AND a.purpose='interview_turn_audio' AND a.state='ready' AND a.deleted_at IS NULL
		AND COALESCE(a.verified_size_bytes,a.expected_size_bytes) <= 24*1024*1024)`,
		input.AudioAssetID, input.OwnerPrincipalID, input.SessionID).Scan(&ready)
	if err != nil {
		return Session{}, err
	}
	if !ready {
		return Session{}, ErrNotFound
	}
	_, err = tx.Exec(ctx, `UPDATE interview_turns SET audio_asset_id=$2,audio_idempotency_key=$3,
		transcript_status='queued',updated_at=NOW() WHERE id=$1`, turnID, input.AudioAssetID, input.IdempotencyKey)
	if err != nil {
		if uniqueViolation(err) {
			return Session{}, ErrConflict
		}
		return Session{}, err
	}
	if err := workqueue.Enqueue(ctx, tx, workqueue.NewJob{
		ID: uuid.NewString(), Kind: JobKind, ResourceID: turnID,
		IdempotencyKey: "interview.turn:" + turnID,
		Payload:        map[string]string{"step": "turn"}, Priority: 10, MaxAttempts: 3,
	}); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return r.Get(ctx, input.OwnerPrincipalID, input.SessionID)
}

func (r *SQLRepository) Finalize(ctx context.Context, input FinalizeInput) (Session, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	row, err := r.sessionRow(ctx, tx, input.OwnerPrincipalID, input.SessionID, true)
	if err != nil {
		return Session{}, err
	}
	if input.EndedAtMs > row.MaxDurationSeconds*1000 {
		return Session{}, ErrDurationLimit
	}
	if row.Status == StatusFinalized {
		if finalizationMatches(row, input) {
			return r.Get(ctx, input.OwnerPrincipalID, input.SessionID)
		}
		return Session{}, ErrConflict
	}
	if (row.Status != StatusRecording && row.Status != StatusFinalizing) ||
		row.RecordingID != input.RecordingID || row.GuestPreviewID != input.GuestPreviewID {
		return Session{}, ErrConflict
	}
	var lastSeq, lastAtMs int
	var ended sql.NullInt64
	if err := tx.QueryRow(ctx, `SELECT seq,asked_at_ms,ended_at_ms FROM interview_turns
		WHERE session_id=$1 ORDER BY seq DESC LIMIT 1 FOR UPDATE`, input.SessionID).Scan(&lastSeq, &lastAtMs, &ended); err != nil {
		return Session{}, err
	}
	if input.EndedAtMs <= lastAtMs {
		return Session{}, ErrConflict
	}
	actualEndedAtMs := input.EndedAtMs
	if ended.Valid {
		actualEndedAtMs = int(ended.Int64)
		if actualEndedAtMs <= lastAtMs || actualEndedAtMs > row.MaxDurationSeconds*1000 {
			return Session{}, ErrConflict
		}
	}
	_, err = tx.Exec(ctx, `UPDATE interview_turns SET ended_at_ms=$2,updated_at=NOW()
		WHERE session_id=$1 AND seq=$3 AND ended_at_ms IS NULL`, input.SessionID, actualEndedAtMs, lastSeq)
	if err != nil {
		return Session{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE interview_sessions SET status='finalized',ended_at_ms=$2,
		finalize_key=$3,updated_at=NOW() WHERE id=$1`, input.SessionID, input.EndedAtMs, input.IdempotencyKey)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return r.Get(ctx, input.OwnerPrincipalID, input.SessionID)
}

func finalizationMatches(row sessionRow, input FinalizeInput) bool {
	return row.FinalizeKey == input.IdempotencyKey &&
		row.EndedAtMs == input.EndedAtMs &&
		row.RecordingID == input.RecordingID &&
		row.GuestPreviewID == input.GuestPreviewID
}

type sessionRow struct {
	ID, OwnerPrincipalID, UserID, Topic, OpeningQuestion, EnglishLevel, Status string
	Interests                                                                  []string
	UsefulWords                                                                []string
	MaxDurationSeconds, EndedAtMs                                              int
	RecordingID, GuestPreviewID, FinalizeKey, Error                            string
	StartedAt                                                                  *time.Time
	CreatedAt, ExpiresAt                                                       time.Time
}

type sessionQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (r *SQLRepository) sessionRow(ctx context.Context, q sessionQuerier, ownerPrincipalID, sessionID string, lock bool) (sessionRow, error) {
	if ownerPrincipalID == "" || sessionID == "" {
		return sessionRow{}, ErrNotFound
	}
	sqlText := `SELECT id,owner_principal_id,COALESCE(user_id,''),topic,opening_question,english_level,
		interests,useful_words,status,max_duration_seconds,started_at,COALESCE(ended_at_ms,0),
		COALESCE(recording_id,''),COALESCE(guest_preview_id,''),COALESCE(finalize_key,''),
		COALESCE(error_message,''),created_at,expires_at
		FROM interview_sessions WHERE id=$1 AND owner_principal_id=$2`
	if lock {
		sqlText += " FOR UPDATE"
	}
	var result sessionRow
	var interests, words []byte
	var started sql.NullTime
	err := q.QueryRow(ctx, sqlText, sessionID, ownerPrincipalID).Scan(
		&result.ID, &result.OwnerPrincipalID, &result.UserID, &result.Topic, &result.OpeningQuestion,
		&result.EnglishLevel, &interests, &words, &result.Status, &result.MaxDurationSeconds,
		&started, &result.EndedAtMs, &result.RecordingID, &result.GuestPreviewID,
		&result.FinalizeKey, &result.Error, &result.CreatedAt, &result.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessionRow{}, ErrNotFound
	}
	if err != nil {
		return sessionRow{}, err
	}
	if started.Valid {
		result.StartedAt = &started.Time
	}
	if err := json.Unmarshal(interests, &result.Interests); err != nil {
		return sessionRow{}, err
	}
	if err := json.Unmarshal(words, &result.UsefulWords); err != nil {
		return sessionRow{}, err
	}
	return result, nil
}

func (r *SQLRepository) view(ctx context.Context, row sessionRow) (Session, error) {
	view := Session{ID: row.ID, Status: row.Status, Topic: row.Topic,
		OpeningQuestion: row.OpeningQuestion, UsefulWords: row.UsefulWords,
		Candidates: []Candidate{}, Turns: []Turn{}, MaxDurationSeconds: row.MaxDurationSeconds,
		Error: row.Error, CreatedAt: row.CreatedAt.UTC()}
	if view.UsefulWords == nil {
		view.UsefulWords = []string{}
	}
	rows, err := r.db.Query(ctx, `SELECT id,question,source FROM interview_candidates
		WHERE session_id=$1 AND consumed_by_turn_seq IS NULL
		ORDER BY CASE WHEN source='adaptive' THEN 0 ELSE 1 END, created_at ASC`, row.ID)
	if err != nil {
		return Session{}, err
	}
	for rows.Next() {
		var candidate Candidate
		if err := rows.Scan(&candidate.ID, &candidate.Question, &candidate.Source); err != nil {
			rows.Close()
			return Session{}, err
		}
		view.Candidates = append(view.Candidates, candidate)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Session{}, err
	}
	rows, err = r.db.Query(ctx, `SELECT t.seq,t.question,t.asked_at_ms,t.ended_at_ms,
		COALESCE(t.provisional_transcript,''),t.transcript_status,
		COALESCE(c.source,'opening') FROM interview_turns t
		LEFT JOIN interview_candidates c ON c.id=t.source_candidate_id
		WHERE t.session_id=$1 ORDER BY t.seq`, row.ID)
	if err != nil {
		return Session{}, err
	}
	for rows.Next() {
		var turn Turn
		var ended sql.NullInt64
		if err := rows.Scan(&turn.Seq, &turn.Question, &turn.AskedAtMs, &ended,
			&turn.ProvisionalTranscript, &turn.TranscriptStatus, &turn.QuestionSource); err != nil {
			rows.Close()
			return Session{}, err
		}
		if ended.Valid {
			value := int(ended.Int64)
			turn.EndedAtMs = &value
		}
		view.CurrentTurnSeq = turn.Seq
		view.Turns = append(view.Turns, turn)
	}
	err = rows.Err()
	rows.Close()
	return view, err
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func normalizedCandidateKey(question string) string { return strings.TrimSpace(questionKey(question)) }
