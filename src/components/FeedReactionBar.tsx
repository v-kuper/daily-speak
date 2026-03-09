"use client";

import {
  FEED_REACTION_EMOJI,
  FEED_REACTION_VALUES,
  type FeedReaction,
  type FeedReactionSummary
} from "../lib/data";

type FeedReactionBarProps = {
  reactions: FeedReactionSummary;
  onReact: (reaction: FeedReaction | null) => void;
  disabled?: boolean;
};

export default function FeedReactionBar({ reactions, onReact, disabled = false }: FeedReactionBarProps) {
  return (
    <div className="feed-reactions">
      {FEED_REACTION_VALUES.map((reaction) => {
        const isActive = reactions.currentReaction === reaction;
        return (
          <button
            key={reaction}
            className={`feed-reaction-btn ${isActive ? "active" : ""}`}
            onClick={() => onReact(isActive ? null : reaction)}
            disabled={disabled}
          >
            <span>{FEED_REACTION_EMOJI[reaction]}</span>
            <span>{reactions.counts[reaction] ?? 0}</span>
          </button>
        );
      })}
    </div>
  );
}
