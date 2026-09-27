package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"daily-speaking-practice/backend/internal/feed"
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
	posts, err := s.feedService.ListPosts(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed posts."})
		return
	}
	response := make([]feedPostResponse, 0, len(posts))
	for _, post := range posts {
		response = append(response, toFeedPostResponse(post))
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": response})
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
	if strings.TrimSpace(payload.RecordingID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Recording ID is required."})
		return
	}
	post, created, err := s.feedService.PublishRecording(r.Context(), user.ID, payload.RecordingID)
	if errors.Is(err, feed.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Recording not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to publish recording."})
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"post": toFeedPostResponse(post)})
}

func (s *Server) routeFeedPostPath(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.handleFeedThread(w, r, pathUnescape(parts[0]))
		return
	}
	if len(parts) == 2 && parts[1] == "replies" && r.Method == http.MethodPost {
		s.handleCreateFeedReply(w, r, pathUnescape(parts[0]))
		return
	}
	if len(parts) == 2 && parts[1] == "reactions" && r.Method == http.MethodPost {
		s.handleReaction(w, r, feed.PostReaction, pathUnescape(parts[0]))
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
}

func (s *Server) routeFeedReplyPath(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 2 && parts[1] == "reactions" && r.Method == http.MethodPost {
		s.handleReaction(w, r, feed.ReplyReaction, pathUnescape(parts[0]))
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
}

func (s *Server) handleFeedThread(w http.ResponseWriter, r *http.Request, postID string) {
	user, ok := s.authorizedUser(w, r, "api.feed.posts.by-id.get")
	if !ok {
		return
	}
	if strings.TrimSpace(postID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Post ID is required."})
		return
	}
	thread, err := s.feedService.GetThread(r.Context(), user.ID, postID)
	if errors.Is(err, feed.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Feed post not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load feed thread."})
		return
	}
	replies := make([]feedReplyResponse, 0, len(thread.Replies))
	for _, reply := range thread.Replies {
		replies = append(replies, toFeedReplyResponse(reply))
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": toFeedPostResponse(thread.Post), "replies": replies})
}

func (s *Server) handleCreateFeedReply(w http.ResponseWriter, r *http.Request, postID string) {
	user, ok := s.authorizedUser(w, r, "api.feed.posts.replies.post")
	if !ok {
		return
	}
	if strings.TrimSpace(postID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Post ID is required."})
		return
	}
	var payload map[string]any
	_ = json.NewDecoder(r.Body).Decode(&payload)
	result, err := s.feedService.CreateReply(r.Context(), feed.CreateReplyInput{
		PostID: postID, UserID: user.ID, UserEmail: user.Email, IsSubscriber: user.IsSubscriber,
		Duration: parseIntAny(payload["duration"]), AudioDataURL: stringAny(payload["audioDataUrl"]),
		Timestamp: stringAny(payload["timestamp"]),
	})
	switch {
	case errors.Is(err, feed.ErrInvalidReply):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Reply duration is invalid."})
	case errors.Is(err, feed.ErrVoiceReplyRequired):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Voice reply is required."})
	case errors.Is(err, feed.ErrSubscriberTooLong):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Subscribers can save recordings up to 10:00 per session."})
	case errors.Is(err, feed.ErrFreeQuotaExceeded):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Weekly free limit exceeded."})
	case errors.Is(err, feed.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Feed post not found."})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save voice reply."})
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"reply": toFeedReplyResponse(result.Reply), "quota": result.Quota})
	}
}

func (s *Server) handleReaction(w http.ResponseWriter, r *http.Request, target feed.ReactionTarget, targetID string) {
	scope := "api.feed.posts.reactions.post"
	idName := "Post ID"
	notFound := "Feed post not found."
	failed := "Failed to update post reaction."
	if target == feed.ReplyReaction {
		scope = "api.feed.replies.reactions.post"
		idName = "Reply ID"
		notFound = "Feed reply not found."
		failed = "Failed to update reply reaction."
	}
	user, ok := s.authorizedUser(w, r, scope)
	if !ok {
		return
	}
	if strings.TrimSpace(targetID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": idName + " is required."})
		return
	}
	var payload map[string]any
	_ = json.NewDecoder(r.Body).Decode(&payload)
	raw, present := payload["reaction"]
	if !present {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Reaction is required."})
		return
	}
	var reaction *string
	if raw != nil && strings.TrimSpace(stringAny(raw)) != "" {
		value := stringAny(raw)
		reaction = &value
	}
	summary, err := s.feedService.SetReaction(r.Context(), target, targetID, user.ID, reaction)
	switch {
	case errors.Is(err, feed.ErrInvalidReaction):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Reaction is invalid."})
	case errors.Is(err, feed.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": notFound})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": failed})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"reactions": summary})
	}
}

func toFeedPostResponse(post feed.Post) feedPostResponse {
	return feedPostResponse{
		ID: post.ID, SourceRecordingID: post.SourceRecordingID, Topic: post.Topic,
		Duration: post.Duration, Transcript: post.Transcript, PracticeType: post.PracticeType,
		AudioDataURL: post.AudioDataURL, PhotoDataURL: post.PhotoDataURL, PhotoObject: post.PhotoObject,
		SourceTimestamp: post.SourceTimestamp.UTC().Format(time.RFC3339Nano),
		CreatedAt:       post.CreatedAt.UTC().Format(time.RFC3339Nano), AuthorMaskedEmail: post.AuthorMaskedEmail,
		ReplyCount: post.ReplyCount, Reactions: post.Reactions,
	}
}

func toFeedReplyResponse(reply feed.Reply) feedReplyResponse {
	return feedReplyResponse{
		ID: reply.ID, PostID: reply.PostID, Duration: reply.Duration, AudioDataURL: reply.AudioDataURL,
		Timestamp: reply.Timestamp.UTC().Format(time.RFC3339Nano), CreatedAt: reply.CreatedAt.UTC().Format(time.RFC3339Nano),
		AuthorMaskedEmail: reply.AuthorMaskedEmail, Reactions: reply.Reactions,
	}
}

func pathUnescape(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}
