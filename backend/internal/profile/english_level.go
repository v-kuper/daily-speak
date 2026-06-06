package profile

import (
	"context"
	"errors"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/domain"
)

func GetEnglishLevel(ctx context.Context, database *db.DB, userID string) (string, error) {
	var raw *string
	if err := database.QueryRow(ctx, `SELECT english_level FROM users WHERE id = $1 LIMIT 1`, userID).Scan(&raw); err != nil {
		return "", err
	}
	if raw == nil {
		return domain.DefaultEnglishLevel, nil
	}
	return domain.NormalizeEnglishLevel(*raw), nil
}

func SaveEnglishLevel(ctx context.Context, database *db.DB, userID string, level string) (string, error) {
	normalized, ok := domain.ParseEnglishLevel(level)
	if !ok {
		return "", errors.New("English level is invalid.")
	}
	_, err := database.Exec(ctx, `UPDATE users SET english_level = $2 WHERE id = $1`, userID, normalized)
	if err != nil {
		return "", err
	}
	return normalized, nil
}
