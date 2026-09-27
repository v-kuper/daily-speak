package feed

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (repository *SQLRepository) SetReaction(ctx context.Context, target ReactionTarget, targetID string, userID string, reaction *string) (ReactionSummary, error) {
	table, idColumn, existsTable, ok := reactionTables(target)
	if !ok {
		return ReactionSummary{}, ErrInvalidRequest
	}
	var existingID string
	err := repository.db.QueryRow(ctx, `SELECT id FROM `+existsTable+` WHERE id = $1 LIMIT 1`, targetID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReactionSummary{}, ErrNotFound
	}
	if err != nil {
		return ReactionSummary{}, err
	}
	if reaction == nil {
		_, err = repository.db.Exec(ctx, `DELETE FROM `+table+` WHERE `+idColumn+` = $1 AND user_id = $2`, targetID, userID)
	} else {
		_, err = repository.db.Exec(ctx, `
			INSERT INTO `+table+` (`+idColumn+`, user_id, reaction)
			VALUES ($1, $2, $3)
			ON CONFLICT (`+idColumn+`, user_id) DO UPDATE
			  SET reaction = EXCLUDED.reaction,
			      created_at = NOW()`, targetID, userID, *reaction)
	}
	if err != nil {
		return ReactionSummary{}, err
	}
	summaries, err := repository.reactionSummaries(ctx, target, []string{targetID}, userID)
	return summaries[targetID], err
}

func (repository *SQLRepository) reactionSummaries(ctx context.Context, target ReactionTarget, ids []string, userID string) (map[string]ReactionSummary, error) {
	out := make(map[string]ReactionSummary, len(ids))
	for _, id := range ids {
		out[id] = EmptyReactionSummary()
	}
	if len(ids) == 0 {
		return out, nil
	}
	table, idColumn, _, ok := reactionTables(target)
	if !ok {
		return nil, ErrInvalidRequest
	}
	countRows, err := repository.db.Query(ctx, `
		SELECT `+idColumn+`, reaction, COUNT(*)::int
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
	userRows, err := repository.db.Query(ctx, `
		SELECT `+idColumn+`, reaction
		FROM `+table+`
		WHERE `+idColumn+` = ANY($1::text[]) AND user_id = $2`, ids, userID)
	if err != nil {
		return nil, err
	}
	defer userRows.Close()
	for userRows.Next() {
		var id, reaction string
		if err := userRows.Scan(&id, &reaction); err != nil {
			return nil, err
		}
		if normalized, valid := NormalizeReaction(reaction); valid {
			summary := out[id]
			summary.CurrentReaction = &normalized
			out[id] = summary
		}
	}
	return out, userRows.Err()
}

func reactionTables(target ReactionTarget) (string, string, string, bool) {
	switch target {
	case PostReaction:
		return "feed_post_reactions", "post_id", "feed_posts", true
	case ReplyReaction:
		return "feed_reply_reactions", "reply_id", "feed_replies", true
	default:
		return "", "", "", false
	}
}
