package feed

import (
	"context"
	"strings"

	"daily-speaking-practice/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

var reactionSet = map[string]struct{}{
	"like": {}, "love": {}, "fire": {}, "laugh": {}, "support": {},
}

type ReactionCounts struct {
	Like    int `json:"like"`
	Love    int `json:"love"`
	Fire    int `json:"fire"`
	Laugh   int `json:"laugh"`
	Support int `json:"support"`
}

type ReactionSummary struct {
	Counts          ReactionCounts `json:"counts"`
	CurrentReaction *string        `json:"currentReaction"`
}

func NormalizeReaction(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	_, ok := reactionSet[normalized]
	return normalized, ok
}

func EmptyReactionSummary() ReactionSummary {
	return ReactionSummary{Counts: ReactionCounts{}}
}

func PostReactionSummaries(ctx context.Context, database *db.DB, postIDs []string, userID string) (map[string]ReactionSummary, error) {
	return reactionSummaries(ctx, database, "feed_post_reactions", "post_id", postIDs, userID)
}

func ReplyReactionSummaries(ctx context.Context, database *db.DB, replyIDs []string, userID string) (map[string]ReactionSummary, error) {
	return reactionSummaries(ctx, database, "feed_reply_reactions", "reply_id", replyIDs, userID)
}

func reactionSummaries(ctx context.Context, database *db.DB, table string, idColumn string, ids []string, userID string) (map[string]ReactionSummary, error) {
	out := map[string]ReactionSummary{}
	for _, id := range ids {
		out[id] = EmptyReactionSummary()
	}
	if len(ids) == 0 {
		return out, nil
	}

	countRows, err := database.Query(ctx, `
		SELECT `+idColumn+` AS target_id, reaction, COUNT(*)::int AS reaction_count
		FROM `+table+`
		WHERE `+idColumn+` = ANY($1::text[])
		GROUP BY `+idColumn+`, reaction`, ids)
	if err != nil {
		return nil, err
	}
	defer countRows.Close()
	for countRows.Next() {
		var id, reaction string
		var count int
		if err := countRows.Scan(&id, &reaction, &count); err != nil {
			return nil, err
		}
		summary := out[id]
		fillCount(&summary.Counts, reaction, count)
		out[id] = summary
	}
	if err := countRows.Err(); err != nil {
		return nil, err
	}

	userRows, err := database.Query(ctx, `
		SELECT `+idColumn+` AS target_id, reaction
		FROM `+table+`
		WHERE `+idColumn+` = ANY($1::text[])
		  AND user_id = $2`, ids, userID)
	if err != nil {
		return nil, err
	}
	defer userRows.Close()
	for userRows.Next() {
		var id, reaction string
		if err := userRows.Scan(&id, &reaction); err != nil {
			return nil, err
		}
		if normalized, ok := NormalizeReaction(reaction); ok {
			summary := out[id]
			summary.CurrentReaction = &normalized
			out[id] = summary
		}
	}
	if err := userRows.Err(); err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	return out, nil
}

func fillCount(counts *ReactionCounts, reaction string, count int) {
	if count < 0 {
		count = 0
	}
	switch reaction {
	case "like":
		counts.Like = count
	case "love":
		counts.Love = count
	case "fire":
		counts.Fire = count
	case "laugh":
		counts.Laugh = count
	case "support":
		counts.Support = count
	}
}
