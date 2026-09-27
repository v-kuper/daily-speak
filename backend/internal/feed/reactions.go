package feed

import (
	"strings"
)

var reactionSet = map[string]struct{}{
	"like": {}, "love": {}, "fire": {}, "laugh": {}, "support": {},
}

func NormalizeReaction(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	_, ok := reactionSet[normalized]
	return normalized, ok
}

func EmptyReactionSummary() ReactionSummary {
	return ReactionSummary{Counts: ReactionCounts{}}
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
