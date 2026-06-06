package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/domain"
	"daily-speaking-practice/backend/internal/feed"
	"daily-speaking-practice/backend/internal/quota"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type feedPostResponse struct {
	ID                string               `json:"id"`
	SourceRecordingID string               `json:"sourceRecordingId"`
	Topic             string               `json:"topic"`
	Duration          int                  `json:"duration"`
	Transcript        string               `json:"transcript"`
	PracticeType      string               `json:"practiceType"`
	AudioDataURL      *string              `json:"audioDataUrl"`
	PhotoDataURL      *string              `json:"photoDataUrl"`
	PhotoObject       *string              `json:"photoObject"`
	SourceTimestamp   string               `json:"sourceTimestamp"`
	CreatedAt         string               `json:"createdAt"`
	AuthorMaskedEmail string               `json:"authorMaskedEmail"`
	ReplyCount        int                  `json:"replyCount"`
	Reactions         feed.ReactionSummary `json:"reactions"`
}

type feedReplyResponse struct {
	ID                string               `json:"id"`
	PostID            string               `json:"postId"`
	Duration          int                  `json:"duration"`
	AudioDataURL      *string              `json:"audioDataUrl"`
	Timestamp         string               `json:"timestamp"`
	CreatedAt         string               `json:"createdAt"`
	AuthorMaskedEmail string               `json:"authorMaskedEmail"`
	Reactions         feed.ReactionSummary `json:"reactions"`
}

func (s *Server) handleFeedPosts(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.feed.posts.get")
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed posts."})
		return
	}
	defer rows.Close()
	rawPosts := []feedPostRow{}
	postIDs := []string{}
	for rows.Next() {
		row, err := scanFeedPost(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed posts."})
			return
		}
		rawPosts = append(rawPosts, row)
		postIDs = append(postIDs, row.ID)
	}
	reactions, err := feed.PostReactionSummaries(r.Context(), s.db, postIDs, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed posts."})
		return
	}
	posts := []feedPostResponse{}
	for _, row := range rawPosts {
		posts = append(posts, toFeedPostResponse(row, reactions[row.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

func (s *Server) handleCreateFeedPost(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authorizedUser(w, r, "api.feed.posts.post")
	if !ok {
		return
	}
	var payload struct {
		RecordingID string `json:"recordingId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	recordingID := strings.TrimSpace(payload.RecordingID)
	if recordingID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording ID is required."})
		return
	}

	var recording struct {
		ID           string
		Topic        string
		Duration     int
		Transcript   string
		PracticeType string
		AudioDataURL *string
		PhotoDataURL *string
		PhotoObject  *string
		Timestamp    time.Time
	}
	err := s.db.QueryRow(r.Context(), `
		SELECT id, topic, duration, transcript, practice_type, audio_data_url, photo_data_url, photo_object, timestamp
		FROM recordings
		WHERE id = $1 AND user_id = $2
		LIMIT 1`, recordingID, user.ID).Scan(&recording.ID, &recording.Topic, &recording.Duration, &recording.Transcript, &recording.PracticeType, &recording.AudioDataURL, &recording.PhotoDataURL, &recording.PhotoObject, &recording.Timestamp)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to publish recording."})
		return
	}

	newID := uuid.NewString()
	var postID string
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO feed_posts
		  (id, user_id, source_recording_id, topic, duration, practice_type, audio_data_url, photo_data_url, photo_object, transcript, source_timestamp)
		VALUES
		  ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (user_id, source_recording_id) DO NOTHING
		RETURNING id`,
		newID,
		user.ID,
		recording.ID,
		truncateRunes(recording.Topic, 300),
		domain.ToNonNegativeInt(recording.Duration),
		domain.NormalizePracticeType(recording.PracticeType),
		normalizeOptionalAudio(recording.AudioDataURL, false),
		normalizeOptionalPhoto(recording.PhotoDataURL),
		normalizeOptionalPhotoObject(recording.PhotoObject),
		recording.Transcript,
		recording.Timestamp,
	).Scan(&postID)
	created := true
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
		err = s.db.QueryRow(r.Context(), `
			SELECT id
			FROM feed_posts
			WHERE user_id = $1 AND source_recording_id = $2
			LIMIT 1`, user.ID, recording.ID).Scan(&postID)
	}
	if err != nil || postID == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to publish recording."})
		return
	}
	post, err := s.getFeedPostByID(r, postID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to publish recording."})
		return
	}
	reactions, err := feed.PostReactionSummaries(r.Context(), s.db, []string{post.ID}, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to publish recording."})
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"post": toFeedPostResponse(post, reactions[post.ID])})
}

func (s *Server) routeFeedPostPath(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		postID := pathUnescape(parts[0])
		s.handleFeedThread(w, r, postID)
		return
	}
	if len(parts) == 2 && parts[1] == "replies" && r.Method == http.MethodPost {
		s.handleCreateFeedReply(w, r, pathUnescape(parts[0]))
		return
	}
	if len(parts) == 2 && parts[1] == "reactions" && r.Method == http.MethodPost {
		s.handlePostReaction(w, r, pathUnescape(parts[0]))
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
}

func (s *Server) routeFeedReplyPath(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 2 && parts[1] == "reactions" && r.Method == http.MethodPost {
		s.handleReplyReaction(w, r, pathUnescape(parts[0]))
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
}

func (s *Server) handleFeedThread(w http.ResponseWriter, r *http.Request, postID string) {
	user, ok := s.authorizedUser(w, r, "api.feed.posts.by-id.get")
	if !ok {
		return
	}
	postID = strings.TrimSpace(postID)
	if postID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Post ID is required."})
		return
	}
	post, err := s.getFeedPostByID(r, postID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Feed post not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed thread."})
		return
	}
	replyRows, err := s.db.Query(r.Context(), `
		SELECT r.id, r.post_id, r.duration, r.audio_data_url, r.timestamp, r.created_at, u.email AS author_email
		FROM feed_replies r
		JOIN users u ON u.id = r.user_id
		WHERE r.post_id = $1
		ORDER BY r.created_at ASC`, postID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed thread."})
		return
	}
	defer replyRows.Close()
	rawReplies := []feedReplyRow{}
	replyIDs := []string{}
	for replyRows.Next() {
		row, err := scanFeedReply(replyRows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed thread."})
			return
		}
		rawReplies = append(rawReplies, row)
		replyIDs = append(replyIDs, row.ID)
	}
	postReactions, err := feed.PostReactionSummaries(r.Context(), s.db, []string{post.ID}, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed thread."})
		return
	}
	replyReactions, err := feed.ReplyReactionSummaries(r.Context(), s.db, replyIDs, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed thread."})
		return
	}
	replies := []feedReplyResponse{}
	for _, row := range rawReplies {
		replies = append(replies, toFeedReplyResponse(row, replyReactions[row.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": toFeedPostResponse(post, postReactions[post.ID]), "replies": replies})
}

func (s *Server) handleCreateFeedReply(w http.ResponseWriter, r *http.Request, postID string) {
	user, ok := s.authorizedUser(w, r, "api.feed.posts.replies.post")
	if !ok {
		return
	}
	postID = strings.TrimSpace(postID)
	if postID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Post ID is required."})
		return
	}
	var existingID string
	err := s.db.QueryRow(r.Context(), `SELECT id FROM feed_posts WHERE id = $1 LIMIT 1`, postID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Feed post not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save voice reply."})
		return
	}
	var payload map[string]any
	_ = json.NewDecoder(r.Body).Decode(&payload)
	duration := parseIntAny(payload["duration"])
	if duration <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Reply duration is invalid."})
		return
	}
	audio := domain.ParseIncomingAudioDataURL(stringAny(payload["audioDataUrl"]))
	if audio == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Voice reply is required."})
		return
	}
	qBefore, err := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &user.IsSubscriber)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save voice reply."})
		return
	}
	if quotaError := recordingQuotaError(qBefore, duration); quotaError != nil {
		writeJSON(w, quotaError.status, map[string]string{"error": quotaError.message})
		return
	}
	replyID := uuid.NewString()
	savedAudio, err := saveAudioFile("feed-replies", user.ID, replyID, audio)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save voice reply."})
		return
	}
	cleanupAudio := savedAudio.absolutePath
	defer func() {
		if cleanupAudio != "" {
			_ = os.Remove(cleanupAudio)
		}
	}()
	timestamp := domain.ParseTimestamp(stringAny(payload["timestamp"]))
	var row feedReplyRow
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO feed_replies (id, post_id, user_id, duration, audio_data_url, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, post_id, duration, audio_data_url, timestamp, created_at`,
		replyID, postID, user.ID, duration, savedAudio.publicURL, timestamp,
	).Scan(&row.ID, &row.PostID, &row.Duration, &row.AudioDataURL, &row.Timestamp, &row.CreatedAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save voice reply."})
		return
	}
	cleanupAudio = ""
	row.AuthorEmail = user.Email
	q, err := quota.GetRecordingQuota(r.Context(), s.db, user.ID, &user.IsSubscriber)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save voice reply."})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"reply": toFeedReplyResponse(row, feed.EmptyReactionSummary()), "quota": q})
}

func (s *Server) handlePostReaction(w http.ResponseWriter, r *http.Request, postID string) {
	s.handleReaction(w, r, reactionConfig{
		scope:       "api.feed.posts.reactions.post",
		idName:      "Post ID",
		targetID:    strings.TrimSpace(postID),
		table:       "feed_post_reactions",
		targetCol:   "post_id",
		existsTable: "feed_posts",
		notFound:    "Feed post not found.",
		failed:      "Failed to update post reaction.",
		summary: func(ids []string, userID string) (map[string]feed.ReactionSummary, error) {
			return feed.PostReactionSummaries(r.Context(), s.db, ids, userID)
		},
	})
}

func (s *Server) handleReplyReaction(w http.ResponseWriter, r *http.Request, replyID string) {
	s.handleReaction(w, r, reactionConfig{
		scope:       "api.feed.replies.reactions.post",
		idName:      "Reply ID",
		targetID:    strings.TrimSpace(replyID),
		table:       "feed_reply_reactions",
		targetCol:   "reply_id",
		existsTable: "feed_replies",
		notFound:    "Feed reply not found.",
		failed:      "Failed to update reply reaction.",
		summary: func(ids []string, userID string) (map[string]feed.ReactionSummary, error) {
			return feed.ReplyReactionSummaries(r.Context(), s.db, ids, userID)
		},
	})
}

type reactionConfig struct {
	scope       string
	idName      string
	targetID    string
	table       string
	targetCol   string
	existsTable string
	notFound    string
	failed      string
	summary     func(ids []string, userID string) (map[string]feed.ReactionSummary, error)
}

func (s *Server) handleReaction(w http.ResponseWriter, r *http.Request, config reactionConfig) {
	user, ok := s.authorizedUser(w, r, config.scope)
	if !ok {
		return
	}
	if config.targetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": config.idName + " is required."})
		return
	}
	var existingID string
	err := s.db.QueryRow(r.Context(), `SELECT id FROM `+config.existsTable+` WHERE id = $1 LIMIT 1`, config.targetID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": config.notFound})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": config.failed})
		return
	}
	var payload map[string]any
	_ = json.NewDecoder(r.Body).Decode(&payload)
	reactionRaw, hasReaction := payload["reaction"]
	if !hasReaction {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Reaction is required."})
		return
	}
	if reactionRaw == nil || strings.TrimSpace(stringAny(reactionRaw)) == "" {
		_, err = s.db.Exec(r.Context(), `DELETE FROM `+config.table+` WHERE `+config.targetCol+` = $1 AND user_id = $2`, config.targetID, user.ID)
	} else {
		reaction, valid := feed.NormalizeReaction(stringAny(reactionRaw))
		if !valid {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Reaction is invalid."})
			return
		}
		_, err = s.db.Exec(r.Context(), `
			INSERT INTO `+config.table+` (`+config.targetCol+`, user_id, reaction)
			VALUES ($1, $2, $3)
			ON CONFLICT (`+config.targetCol+`, user_id) DO UPDATE
			  SET reaction = EXCLUDED.reaction,
			      created_at = NOW()`, config.targetID, user.ID, reaction)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": config.failed})
		return
	}
	reactions, err := config.summary([]string{config.targetID}, user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": config.failed})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reactions": reactions[config.targetID]})
}

type feedPostRow struct {
	ID                string
	SourceRecordingID string
	Topic             string
	Duration          int
	PracticeType      string
	AudioDataURL      *string
	PhotoDataURL      *string
	PhotoObject       *string
	Transcript        string
	SourceTimestamp   time.Time
	CreatedAt         time.Time
	AuthorEmail       string
	ReplyCount        int
}

type feedReplyRow struct {
	ID           string
	PostID       string
	Duration     int
	AudioDataURL *string
	Timestamp    time.Time
	CreatedAt    time.Time
	AuthorEmail  string
}

type scanner interface {
	Scan(dest ...any) error
}

func scanFeedPost(row scanner) (feedPostRow, error) {
	var out feedPostRow
	err := row.Scan(&out.ID, &out.SourceRecordingID, &out.Topic, &out.Duration, &out.PracticeType, &out.AudioDataURL, &out.PhotoDataURL, &out.PhotoObject, &out.Transcript, &out.SourceTimestamp, &out.CreatedAt, &out.AuthorEmail, &out.ReplyCount)
	return out, err
}

func scanFeedReply(row scanner) (feedReplyRow, error) {
	var out feedReplyRow
	err := row.Scan(&out.ID, &out.PostID, &out.Duration, &out.AudioDataURL, &out.Timestamp, &out.CreatedAt, &out.AuthorEmail)
	return out, err
}

func (s *Server) getFeedPostByID(r *http.Request, postID string) (feedPostRow, error) {
	return scanFeedPost(s.db.QueryRow(r.Context(), `
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
}

func toFeedPostResponse(row feedPostRow, reactions feed.ReactionSummary) feedPostResponse {
	return feedPostResponse{
		ID:                row.ID,
		SourceRecordingID: row.SourceRecordingID,
		Topic:             row.Topic,
		Duration:          domain.ToNonNegativeInt(row.Duration),
		Transcript:        row.Transcript,
		PracticeType:      domain.NormalizePracticeType(row.PracticeType),
		AudioDataURL:      normalizeOptionalAudio(row.AudioDataURL, false),
		PhotoDataURL:      normalizeOptionalPhoto(row.PhotoDataURL),
		PhotoObject:       normalizeOptionalPhotoObject(row.PhotoObject),
		SourceTimestamp:   row.SourceTimestamp.UTC().Format(time.RFC3339Nano),
		CreatedAt:         row.CreatedAt.UTC().Format(time.RFC3339Nano),
		AuthorMaskedEmail: domain.MaskEmail(row.AuthorEmail),
		ReplyCount:        domain.ToNonNegativeInt(row.ReplyCount),
		Reactions:         reactions,
	}
}

func toFeedReplyResponse(row feedReplyRow, reactions feed.ReactionSummary) feedReplyResponse {
	return feedReplyResponse{
		ID:                row.ID,
		PostID:            row.PostID,
		Duration:          domain.ToNonNegativeInt(row.Duration),
		AudioDataURL:      normalizeOptionalAudio(row.AudioDataURL, false),
		Timestamp:         row.Timestamp.UTC().Format(time.RFC3339Nano),
		CreatedAt:         row.CreatedAt.UTC().Format(time.RFC3339Nano),
		AuthorMaskedEmail: domain.MaskEmail(row.AuthorEmail),
		Reactions:         reactions,
	}
}

func pathUnescape(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}
