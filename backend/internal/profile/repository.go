package profile

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/db"
)

type SQLRepository struct{ db *db.DB }

func NewSQLRepository(database *db.DB) *SQLRepository {
	return &SQLRepository{db: database}
}

func (r *SQLRepository) EnglishLevel(ctx context.Context, userID string) (*string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("profile database is not configured")
	}
	var value *string
	err := r.db.QueryRow(ctx, `SELECT english_level FROM users WHERE id = $1 LIMIT 1`, userID).Scan(&value)
	return value, err
}

func (r *SQLRepository) SaveEnglishLevel(ctx context.Context, userID string, value string) error {
	if r == nil || r.db == nil {
		return errors.New("profile database is not configured")
	}
	_, err := r.db.Exec(ctx, `UPDATE users SET english_level = $2 WHERE id = $1`, userID, value)
	return err
}

func (r *SQLRepository) ReplaceInterests(ctx context.Context, userID string, values []string) error {
	if r == nil || r.db == nil {
		return errors.New("profile database is not configured")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM user_interests WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if len(values) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_interests (user_id, interest_id)
			SELECT $1, interest_id
			FROM UNNEST($2::text[]) AS t(interest_id)
			ON CONFLICT DO NOTHING`, userID, values); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *SQLRepository) Interests(ctx context.Context, userID string) ([]string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("profile database is not configured")
	}
	rows, err := r.db.Query(ctx, `
		SELECT interest_id FROM user_interests
		WHERE user_id = $1 ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
