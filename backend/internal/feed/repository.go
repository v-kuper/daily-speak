package feed

import (
	"context"
	"errors"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/db"
	"daily-speaking-practice/backend/internal/media"
	"daily-speaking-practice/backend/internal/practice"
	"daily-speaking-practice/backend/internal/quota"
	"github.com/jackc/pgx/v5"
)

type SQLRepository struct {
	db *db.DB
}

func NewSQLRepository(database *db.DB) *SQLRepository {
	return &SQLRepository{db: database}
}

func (repository *SQLRepository) ListPosts(ctx context.Context, viewerID string) ([]Post, error) {
	rows, err := repository.db.Query(ctx, `
		SELECT
		  p.id, p.source_recording_id, p.topic, p.duration, p.practice_type,
		  p.audio_data_url, p.photo_data_url, p.photo_object, p.transcript,
		  p.source_timestamp, p.created_at, u.email AS author_email,
		  COALESCE(COUNT(r.id), 0)::int AS reply_count
		FROM feed_posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN feed_replies r ON r.post_id = p.id
		GROUP BY p.id, u.email
		ORDER BY p.created_at DESC
		LIMIT 120`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	posts := []Post{}
	ids := []string{}
	for rows.Next() {
		post, scanErr := scanPost(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		posts = append(posts, post)
		ids = append(ids, post.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reactions, err := repository.reactionSummaries(ctx, PostReaction, ids, viewerID)
	if err != nil {
		return nil, err
	}
	for index := range posts {
		posts[index].Reactions = reactions[posts[index].ID]
	}
	return posts, nil
}

func (repository *SQLRepository) PublishRecording(ctx context.Context, userID string, recordingID string, postID string) (Post, bool, error) {
	var source struct {
		ID, Topic, Transcript, PracticeType string
		Duration                            int
		AudioDataURL, PhotoDataURL          *string
		PhotoObject                         *string
		Timestamp                           time.Time
	}
	err := repository.db.QueryRow(ctx, `
		SELECT id, topic, duration, transcript, practice_type, audio_data_url, photo_data_url, photo_object, timestamp
		FROM recordings
		WHERE id = $1 AND user_id = $2
		LIMIT 1`, recordingID, userID).Scan(
		&source.ID, &source.Topic, &source.Duration, &source.Transcript, &source.PracticeType,
		&source.AudioDataURL, &source.PhotoDataURL, &source.PhotoObject, &source.Timestamp,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, false, ErrNotFound
	}
	if err != nil {
		return Post{}, false, err
	}

	var persistedID string
	err = repository.db.QueryRow(ctx, `
		INSERT INTO feed_posts
		  (id, user_id, source_recording_id, topic, duration, practice_type, audio_data_url, photo_data_url, photo_object, transcript, source_timestamp)
		VALUES
		  ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (user_id, source_recording_id) DO NOTHING
		RETURNING id`,
		postID, userID, source.ID, truncateRunes(source.Topic, 300), nonNegative(source.Duration),
		practice.NormalizeType(source.PracticeType), normalizeAudio(source.AudioDataURL),
		media.NormalizePhotoDataURL(pointerValue(source.PhotoDataURL)), media.NormalizePhotoObject(pointerValue(source.PhotoObject)),
		source.Transcript, source.Timestamp,
	).Scan(&persistedID)
	created := true
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
		err = repository.db.QueryRow(ctx, `
			SELECT id FROM feed_posts WHERE user_id = $1 AND source_recording_id = $2 LIMIT 1`,
			userID, source.ID,
		).Scan(&persistedID)
	}
	if err != nil {
		return Post{}, false, err
	}
	post, err := repository.getPost(ctx, persistedID)
	if err != nil {
		return Post{}, false, err
	}
	reactions, err := repository.reactionSummaries(ctx, PostReaction, []string{post.ID}, userID)
	if err != nil {
		return Post{}, false, err
	}
	post.Reactions = reactions[post.ID]
	return post, created, nil
}

func (repository *SQLRepository) GetThread(ctx context.Context, postID string, viewerID string) (Thread, error) {
	post, err := repository.getPost(ctx, postID)
	if err != nil {
		return Thread{}, err
	}
	rows, err := repository.db.Query(ctx, `
		SELECT r.id, r.post_id, r.duration, r.audio_data_url, r.timestamp, r.created_at, u.email AS author_email
		FROM feed_replies r
		JOIN users u ON u.id = r.user_id
		WHERE r.post_id = $1
		ORDER BY r.created_at ASC`, postID)
	if err != nil {
		return Thread{}, err
	}
	defer rows.Close()
	replies := []Reply{}
	replyIDs := []string{}
	for rows.Next() {
		reply, scanErr := scanReply(rows)
		if scanErr != nil {
			return Thread{}, scanErr
		}
		replies = append(replies, reply)
		replyIDs = append(replyIDs, reply.ID)
	}
	if err := rows.Err(); err != nil {
		return Thread{}, err
	}
	postReactions, err := repository.reactionSummaries(ctx, PostReaction, []string{post.ID}, viewerID)
	if err != nil {
		return Thread{}, err
	}
	replyReactions, err := repository.reactionSummaries(ctx, ReplyReaction, replyIDs, viewerID)
	if err != nil {
		return Thread{}, err
	}
	post.Reactions = postReactions[post.ID]
	for index := range replies {
		replies[index].Reactions = replyReactions[replies[index].ID]
	}
	return Thread{Post: post, Replies: replies}, nil
}

func (repository *SQLRepository) CreateReply(ctx context.Context, replyID string, postID string, userID string, duration int, audioURL string, timestamp string) (Reply, error) {
	var reply Reply
	err := repository.db.QueryRow(ctx, `
		INSERT INTO feed_replies (id, post_id, user_id, duration, audio_data_url, timestamp)
		SELECT $1, p.id, $3, $4, $5, $6
		FROM feed_posts p
		WHERE p.id = $2
		RETURNING id, post_id, duration, audio_data_url, timestamp, created_at`,
		replyID, postID, userID, duration, audioURL, parseTimestamp(timestamp),
	).Scan(&reply.ID, &reply.PostID, &reply.Duration, &reply.AudioDataURL, &reply.Timestamp, &reply.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reply{}, ErrNotFound
	}
	if err != nil {
		return Reply{}, err
	}
	return reply, nil
}

func (repository *SQLRepository) GetQuota(ctx context.Context, userID string, isSubscriber bool) (Quota, error) {
	value, err := quota.GetRecordingQuota(ctx, repository.db, userID, &isSubscriber)
	if err != nil {
		return Quota{}, err
	}
	return Quota{
		IsSubscriber: value.IsSubscriber, WeeklyLimitSeconds: value.WeeklyLimitSeconds,
		WeeklyUsedSeconds: value.WeeklyUsedSeconds, WeeklyRemainingSeconds: value.WeeklyRemainingSeconds,
		MaxSessionSeconds: value.MaxSessionSeconds,
	}, nil
}

func (repository *SQLRepository) getPost(ctx context.Context, postID string) (Post, error) {
	post, err := scanPost(repository.db.QueryRow(ctx, `
		SELECT
		  p.id, p.source_recording_id, p.topic, p.duration, p.practice_type,
		  p.audio_data_url, p.photo_data_url, p.photo_object, p.transcript,
		  p.source_timestamp, p.created_at, u.email AS author_email,
		  COALESCE(COUNT(r.id), 0)::int AS reply_count
		FROM feed_posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN feed_replies r ON r.post_id = p.id
		WHERE p.id = $1
		GROUP BY p.id, u.email
		LIMIT 1`, postID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	return post, err
}

type scanner interface {
	Scan(...any) error
}

func scanPost(row scanner) (Post, error) {
	var post Post
	var authorEmail string
	err := row.Scan(
		&post.ID, &post.SourceRecordingID, &post.Topic, &post.Duration, &post.PracticeType,
		&post.AudioDataURL, &post.PhotoDataURL, &post.PhotoObject, &post.Transcript,
		&post.SourceTimestamp, &post.CreatedAt, &authorEmail, &post.ReplyCount,
	)
	post.Duration = nonNegative(post.Duration)
	post.ReplyCount = nonNegative(post.ReplyCount)
	post.PracticeType = practice.NormalizeType(post.PracticeType)
	post.AudioDataURL = normalizeAudio(post.AudioDataURL)
	post.PhotoDataURL = media.NormalizePhotoDataURL(pointerValue(post.PhotoDataURL))
	post.PhotoObject = media.NormalizePhotoObject(pointerValue(post.PhotoObject))
	post.AuthorMaskedEmail = maskEmail(authorEmail)
	return post, err
}

func scanReply(row scanner) (Reply, error) {
	var reply Reply
	var authorEmail string
	err := row.Scan(&reply.ID, &reply.PostID, &reply.Duration, &reply.AudioDataURL, &reply.Timestamp, &reply.CreatedAt, &authorEmail)
	reply.Duration = nonNegative(reply.Duration)
	reply.AudioDataURL = normalizeAudio(reply.AudioDataURL)
	reply.AuthorMaskedEmail = maskEmail(authorEmail)
	return reply, err
}

func normalizeAudio(value *string) *string {
	return media.NormalizeStoredGenericAudioSource(pointerValue(value))
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
