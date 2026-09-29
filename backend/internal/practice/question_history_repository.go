package practice

import (
	"context"
	"fmt"

	"daily-speaking-practice/backend/internal/db"
)

// SQLQuestionHistoryRepository keeps exclusions across devices and reads the
// authoritative interview turns, not the paginated recording summary API.
type SQLQuestionHistoryRepository struct{ db *db.DB }

func NewSQLQuestionHistoryRepository(database *db.DB) *SQLQuestionHistoryRepository {
	return &SQLQuestionHistoryRepository{db: database}
}

func (r *SQLQuestionHistoryRepository) ListAvoidQuestions(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT question FROM (
			SELECT t.question, t.created_at AS asked_at
			FROM interview_turns t
			JOIN interview_sessions s ON s.id = t.session_id
			WHERE s.user_id = $1 AND NOT t.skipped
			  AND (BTRIM(COALESCE(t.final_transcript, '')) <> ''
			       OR BTRIM(COALESCE(t.provisional_transcript, '')) <> '')
			UNION ALL
			SELECT r.topic, r.timestamp AS asked_at
			FROM recordings r
			WHERE r.user_id = $1 AND r.practice_type = 'topic'
			  AND BTRIM(r.transcript) <> ''
			UNION ALL
			SELECT d.question, d.created_at AS asked_at
			FROM practice_dismissed_questions d
			WHERE d.user_id = $1
		) history
		ORDER BY asked_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list question history: %w", err)
	}
	defer rows.Close()
	var questions []string
	for rows.Next() {
		var question string
		if err := rows.Scan(&question); err != nil {
			return nil, fmt.Errorf("scan question history: %w", err)
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read question history: %w", err)
	}
	return questions, nil
}

func (r *SQLQuestionHistoryRepository) DismissQuestion(ctx context.Context, userID, question, key string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO practice_dismissed_questions (user_id, question_key, question)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, question_key) DO NOTHING`, userID, key, question)
	if err != nil {
		return fmt.Errorf("dismiss question: %w", err)
	}
	return nil
}
