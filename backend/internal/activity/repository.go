package activity

import (
	"context"
	"errors"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type SQLRepository struct{ db *db.DB }

func NewSQLRepository(database *db.DB) *SQLRepository { return &SQLRepository{db: database} }

func (r *SQLRepository) Append(ctx context.Context, userID string, intervals []Interval) error {
	if r == nil || r.db == nil {
		return errors.New("activity database is not configured")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize interval acceptance for this account across API replicas.
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `SELECT created_at FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccount
		}
		return err
	}
	for _, input := range intervals {
		var prior Interval
		err := tx.QueryRow(ctx, `SELECT kind,started_at,ended_at FROM activity_events WHERE user_id=$1 AND id=$2`, userID, input.ID).
			Scan(&prior.Kind, &prior.StartedAt, &prior.EndedAt)
		if err == nil {
			if prior.Kind != input.Kind || !prior.StartedAt.Equal(input.StartedAt) || !prior.EndedAt.Equal(input.EndedAt) {
				return ErrConflict
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if input.StartedAt.Before(createdAt.Truncate(time.Millisecond)) {
			return ErrInvalid
		}
		rows, err := tx.Query(ctx, `SELECT started_at,ended_at FROM activity_credits
			WHERE user_id=$1 AND started_at<$3 AND ended_at>$2 ORDER BY started_at`, userID, input.StartedAt, input.EndedAt)
		if err != nil {
			return err
		}
		existing := []Interval{}
		for rows.Next() {
			var interval Interval
			if err := rows.Scan(&interval.StartedAt, &interval.EndedAt); err != nil {
				rows.Close()
				return err
			}
			existing = append(existing, interval)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO activity_events(user_id,id,kind,started_at,ended_at) VALUES($1,$2,$3,$4,$5)`,
			userID, input.ID, input.Kind, input.StartedAt, input.EndedAt); err != nil {
			return err
		}
		for index, piece := range uncovered(input, existing) {
			if _, err := tx.Exec(ctx, `INSERT INTO activity_credits(user_id,event_id,piece,kind,started_at,ended_at)
				VALUES($1,$2,$3,$4,$5,$6)`, userID, input.ID, index, input.Kind, piece.StartedAt, piece.EndedAt); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (r *SQLRepository) Summary(ctx context.Context, userID string, window Window) (Summary, error) {
	if r == nil || r.db == nil {
		return Summary{}, errors.New("activity database is not configured")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return Summary{}, err
	}
	result := Summary{Days: []Day{}}
	err = tx.QueryRow(ctx, `SELECT
		COALESCE(SUM(EXTRACT(EPOCH FROM ended_at-started_at)*1000) FILTER(WHERE kind='speaking'),0)::bigint,
		COALESCE(SUM(EXTRACT(EPOCH FROM ended_at-started_at)*1000) FILTER(WHERE historical),0)::bigint
		FROM activity_credits WHERE user_id=$1`, userID).
		Scan(&result.TotalSpeakingMilliseconds, &result.HistoricalSpeakingMilliseconds)
	if err != nil {
		return Summary{}, err
	}
	// Split at local midnight, including days with 23 or 25 hours. UTC dates are
	// never used as a substitute for the learner's calendar.
	rows, err := tx.Query(ctx, `WITH days AS (
		SELECT d::date AS day, d::date::timestamp AT TIME ZONE $4 AS start,
			(d::date+1)::timestamp AT TIME ZONE $4 AS finish
		FROM generate_series(($2::timestamptz AT TIME ZONE $4)::date::timestamp,
			(($3::timestamptz AT TIME ZONE $4)::date-1)::timestamp, INTERVAL '1 day') d
	)
	SELECT to_char(days.day,'YYYY-MM-DD'),
		COALESCE(SUM(EXTRACT(EPOCH FROM LEAST(c.ended_at,days.finish)-GREATEST(c.started_at,days.start))*1000)
			FILTER(WHERE c.kind='speaking'),0)::bigint,
		COALESCE(SUM(EXTRACT(EPOCH FROM LEAST(c.ended_at,days.finish)-GREATEST(c.started_at,days.start))*1000)
			FILTER(WHERE c.kind='review'),0)::bigint
	FROM days JOIN activity_credits c ON c.user_id=$1 AND c.started_at<days.finish AND c.ended_at>days.start
	WHERE c.started_at<$3 AND c.ended_at>$2 GROUP BY days.day ORDER BY days.day`,
		userID, window.From, window.To, window.Timezone)
	if err != nil {
		return Summary{}, err
	}
	for rows.Next() {
		var day Day
		if err := rows.Scan(&day.Date, &day.SpeakingMilliseconds, &day.ReviewMilliseconds); err != nil {
			rows.Close()
			return Summary{}, err
		}
		result.Days = append(result.Days, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Summary{}, err
	}
	return result, tx.Commit(ctx)
}
