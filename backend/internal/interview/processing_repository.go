package interview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/workqueue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PreparationWork struct {
	SessionID, Topic, OpeningQuestion, EnglishLevel string
	Interests                                       []string
}

type TurnWork struct {
	TurnID, SessionID, AudioAssetID, Topic, EnglishLevel, Transcript, Status, SessionStatus string
	Seq                                                                                     int
	History                                                                                 []ContextTurn
	AvoidQuestions                                                                          []string
}

type RefillWork struct {
	SessionID, Topic, EnglishLevel string
	History                        []ContextTurn
	AvoidQuestions                 []string
}

func (r *SQLRepository) LoadPreparation(ctx context.Context, sessionID string) (PreparationWork, bool, error) {
	var work PreparationWork
	var interests []byte
	var status string
	err := r.db.QueryRow(ctx, `SELECT id,topic,opening_question,english_level,interests,status
		FROM interview_sessions WHERE id=$1`, sessionID).Scan(
		&work.SessionID, &work.Topic, &work.OpeningQuestion, &work.EnglishLevel, &interests, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PreparationWork{}, false, nil
	}
	if err != nil {
		return PreparationWork{}, false, err
	}
	if status != StatusPreparing {
		return PreparationWork{}, false, nil
	}
	if err := json.Unmarshal(interests, &work.Interests); err != nil {
		return PreparationWork{}, false, err
	}
	return work, true, nil
}

func (r *SQLRepository) SavePreparation(ctx context.Context, job workqueue.Job, sessionID string, prepared Preparation) error {
	if len(prepared.Questions) != 3 || len(prepared.Vocabulary) != preparationVocabularyCount {
		return ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := requireInterviewLease(ctx, tx, job); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM interview_sessions WHERE id=$1 FOR UPDATE`, sessionID).Scan(&status); err != nil {
		return err
	}
	if status != StatusPreparing {
		return nil
	}
	words := make([]string, 0, len(prepared.Vocabulary))
	for _, item := range prepared.Vocabulary {
		words = append(words, item.Word)
	}
	wordsJSON, _ := json.Marshal(words)
	vocabularyJSON, _ := json.Marshal(prepared.Vocabulary)
	for _, question := range prepared.Questions {
		if _, err := tx.Exec(ctx, `INSERT INTO interview_candidates(id,session_id,question,source)
			VALUES($1,$2,$3,'prepared')`, uuid.NewString(), sessionID, question); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE interview_sessions SET useful_words=$2::jsonb,useful_vocabulary=$3::jsonb,status='ready',updated_at=NOW()
		WHERE id=$1`, sessionID, string(wordsJSON), string(vocabularyJSON)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SQLRepository) LoadTurn(ctx context.Context, turnID string) (TurnWork, bool, error) {
	var work TurnWork
	var nullableTranscript sql.NullString
	err := r.db.QueryRow(ctx, `SELECT t.id,t.session_id,t.seq,COALESCE(t.audio_asset_id,''),
		COALESCE(t.final_transcript,t.provisional_transcript,''),t.transcript_status,s.topic,s.english_level,s.status
		FROM interview_turns t JOIN interview_sessions s ON s.id=t.session_id WHERE t.id=$1 AND NOT t.skipped`, turnID).
		Scan(&work.TurnID, &work.SessionID, &work.Seq, &work.AudioAssetID,
			&nullableTranscript, &work.Status, &work.Topic, &work.EnglishLevel, &work.SessionStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return TurnWork{}, false, nil
	}
	if err != nil {
		return TurnWork{}, false, err
	}
	work.Transcript = nullableTranscript.String
	if work.Status != "ready" && work.AudioAssetID == "" {
		return TurnWork{}, false, ErrNotFound
	}
	rows, err := r.db.Query(ctx, `SELECT seq,question,COALESCE(final_transcript,provisional_transcript,'')
		FROM interview_turns WHERE session_id=$1 AND seq <= $2 AND NOT skipped ORDER BY seq DESC LIMIT 6`, work.SessionID, work.Seq)
	if err != nil {
		return TurnWork{}, false, err
	}
	for rows.Next() {
		var turn ContextTurn
		if err := rows.Scan(&turn.Seq, &turn.Question, &turn.Transcript); err != nil {
			rows.Close()
			return TurnWork{}, false, err
		}
		work.History = append(work.History, turn)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return TurnWork{}, false, err
	}
	for i, j := 0, len(work.History)-1; i < j; i, j = i+1, j-1 {
		work.History[i], work.History[j] = work.History[j], work.History[i]
	}
	work.AvoidQuestions, err = r.allQuestions(ctx, work.SessionID)
	if err != nil {
		return TurnWork{}, false, err
	}
	return work, true, nil
}

func (r *SQLRepository) LoadRefill(ctx context.Context, sessionID string) (RefillWork, bool, error) {
	var work RefillWork
	var status string
	err := r.db.QueryRow(ctx, `SELECT id,topic,english_level,status FROM interview_sessions WHERE id=$1`, sessionID).
		Scan(&work.SessionID, &work.Topic, &work.EnglishLevel, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return RefillWork{}, false, nil
	}
	if err != nil {
		return RefillWork{}, false, err
	}
	if status != StatusRecording {
		return RefillWork{}, false, nil
	}
	var remaining int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM interview_candidates WHERE session_id=$1
		AND consumed_by_turn_seq IS NULL`, sessionID).Scan(&remaining); err != nil {
		return RefillWork{}, false, err
	}
	if remaining >= 3 {
		return RefillWork{}, false, nil
	}
	rows, err := r.db.Query(ctx, `SELECT seq,question,COALESCE(final_transcript,provisional_transcript,'')
		FROM interview_turns WHERE session_id=$1 AND NOT skipped ORDER BY seq DESC LIMIT 6`, sessionID)
	if err != nil {
		return RefillWork{}, false, err
	}
	for rows.Next() {
		var turn ContextTurn
		if err := rows.Scan(&turn.Seq, &turn.Question, &turn.Transcript); err != nil {
			rows.Close()
			return RefillWork{}, false, err
		}
		work.History = append(work.History, turn)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RefillWork{}, false, err
	}
	for i, j := 0, len(work.History)-1; i < j; i, j = i+1, j-1 {
		work.History[i], work.History[j] = work.History[j], work.History[i]
	}
	work.AvoidQuestions, err = r.allQuestions(ctx, sessionID)
	if err != nil {
		return RefillWork{}, false, err
	}
	return work, true, nil
}

func (r *SQLRepository) allQuestions(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT question FROM interview_turns WHERE session_id=$1
		UNION ALL SELECT question FROM interview_candidates WHERE session_id=$1`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	questions := []string{}
	for rows.Next() {
		var question string
		if err := rows.Scan(&question); err != nil {
			return nil, err
		}
		questions = append(questions, question)
	}
	return questions, rows.Err()
}

func (r *SQLRepository) SaveTranscript(ctx context.Context, job workqueue.Job, turnID, transcript string) (string, error) {
	transcript = strings.TrimSpace(transcript)
	if len([]rune(transcript)) > 4000 {
		transcript = truncateRunes(transcript, 4000)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := requireInterviewLease(ctx, tx, job); err != nil {
		return "", err
	}
	result, err := tx.Exec(ctx, `UPDATE interview_turns
		SET provisional_transcript=$2,final_transcript=$2,transcript_status='ready',
		    transcript_origin='turn_batch',updated_at=NOW()
		WHERE id=$1 AND audio_asset_id IS NOT NULL
		  AND NOT skipped
		  AND COALESCE(transcript_origin,'') <> 'turn_realtime'`, turnID, transcript)
	if err != nil {
		return "", err
	}
	canonicalTranscript := transcript
	// A realtime result may win while the fallback job is in flight. In that
	// case its canonical text must remain untouched and must be used for the
	// adaptive follow-up generated by this job.
	if result.RowsAffected() != 1 {
		var origin string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(transcript_origin,''),
			COALESCE(final_transcript,provisional_transcript,'')
			FROM interview_turns WHERE id=$1`, turnID).Scan(&origin, &canonicalTranscript); err != nil {
			return "", err
		}
		if origin != "turn_realtime" {
			return "", ErrNotFound
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return canonicalTranscript, nil
}

func (r *SQLRepository) SaveAdaptive(ctx context.Context, job workqueue.Job, sessionID string, sourceSeq int, question string) (bool, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return false, ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err := requireInterviewLease(ctx, tx, job); err != nil {
		return false, err
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM interview_sessions WHERE id=$1 FOR UPDATE`, sessionID).Scan(&status); err != nil {
		return false, err
	}
	if status != StatusRecording {
		return false, nil
	}
	var currentSeq int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(seq),0) FROM interview_turns WHERE session_id=$1`, sessionID).Scan(&currentSeq); err != nil {
		return false, err
	}
	if sourceSeq < currentSeq-2 {
		return false, nil
	}
	key := normalizedCandidateKey(question)
	rows, err := tx.Query(ctx, `SELECT question FROM interview_turns WHERE session_id=$1
		UNION ALL SELECT question FROM interview_candidates WHERE session_id=$1`, sessionID)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var existing string
		if err := rows.Scan(&existing); err != nil {
			rows.Close()
			return false, err
		}
		if normalizedCandidateKey(existing) == key || questionsOverlap(existing, question) {
			rows.Close()
			return false, nil
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO interview_candidates(id,session_id,question,source,source_turn_seq)
		VALUES($1,$2,$3,'adaptive',$4)`, uuid.NewString(), sessionID, question, sourceSeq)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *SQLRepository) SaveRefill(ctx context.Context, job workqueue.Job, sessionID string, questions []string) (int, error) {
	if len(questions) != 3 {
		return 0, ErrInvalid
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err := requireInterviewLease(ctx, tx, job); err != nil {
		return 0, err
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM interview_sessions WHERE id=$1 FOR UPDATE`, sessionID).Scan(&status); err != nil {
		return 0, err
	}
	if status != StatusRecording {
		return 0, nil
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM interview_candidates WHERE session_id=$1
		AND consumed_by_turn_seq IS NULL`, sessionID).Scan(&remaining); err != nil {
		return 0, err
	}
	if remaining >= 3 {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT question FROM interview_turns WHERE session_id=$1
		UNION ALL SELECT question FROM interview_candidates WHERE session_id=$1`, sessionID)
	if err != nil {
		return 0, err
	}
	seen := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return 0, err
		}
		seen = append(seen, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	added := 0
	for _, question := range questions {
		if remaining >= 3 {
			break
		}
		duplicate := false
		for _, existing := range seen {
			if questionsOverlap(existing, question) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO interview_candidates(id,session_id,question,source)
			VALUES($1,$2,$3,'prepared')`, uuid.NewString(), sessionID, question); err != nil {
			return 0, err
		}
		seen = append(seen, question)
		remaining++
		added++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	if added == 0 && remaining <= 1 {
		return 0, errors.New("interview reserve contains no new questions")
	}
	return added, nil
}

func requireInterviewLease(ctx context.Context, tx pgx.Tx, job workqueue.Job) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT state = 'running' AND lease_token = $2 AND COALESCE(lease_expires_at > NOW(), FALSE)
		FROM processing_jobs WHERE id = $1 FOR SHARE`, job.ID, job.LeaseToken).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return workqueue.ErrLeaseLost
	}
	return err
}

func (r *SQLRepository) QueueWaitMs(ctx context.Context, jobID string) int64 {
	var created time.Time
	if err := r.db.QueryRow(ctx, `SELECT created_at FROM processing_jobs WHERE id=$1`, jobID).Scan(&created); err != nil {
		return -1
	}
	return max(0, time.Since(created).Milliseconds())
}

func (r *SQLRepository) Expire(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `UPDATE interview_sessions
		SET status='failed',error_message='Session expired',updated_at=NOW()
		WHERE status IN ('preparing','ready','recording') AND expires_at <= NOW()`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `UPDATE interview_sessions
		SET status='finalized',updated_at=NOW()
		WHERE status='finalizing' AND expires_at <= NOW()
		AND (recording_id IS NOT NULL OR guest_preview_id IS NOT NULL)`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `DELETE FROM interview_sessions
		WHERE status IN ('failed','cancelled') AND expires_at <= NOW()
		AND recording_id IS NULL AND guest_preview_id IS NULL`)
	return err
}

func (r *SQLRepository) FinalizeFailure(ctx context.Context, tx pgx.Tx, job workqueue.Job, message string) error {
	var payload struct {
		Step string `json:"step"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("decode interview job: %w", err)
	}
	switch payload.Step {
	case "prepare":
		_, err := tx.Exec(ctx, `UPDATE interview_sessions SET status='failed',error_message='Preparation failed',updated_at=NOW()
			WHERE id=$1 AND status='preparing'`, job.ResourceID)
		return err
	case "turn":
		_, err := tx.Exec(ctx, `UPDATE interview_turns SET transcript_status='failed',updated_at=NOW()
			WHERE id=$1 AND NOT skipped AND transcript_status<>'ready'`, job.ResourceID)
		return err
	case "refill":
		return nil
	default:
		return nil
	}
}
