"use client";

import { useEffect, useState } from "react";
import { formatTime } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import { fetchFeedPosts, openFeedThread, reactToFeedPost } from "../store/slices/appSlice";
import FeedReactionBar from "./FeedReactionBar";

export default function FeedScreen() {
  const dispatch = useAppDispatch();
  const { feedPosts, feedPostsStatus, feedPostsError, feedReactionStatus, feedReactionError } = useAppSelector(
    (state) => state.app
  );
  const [expandedTranscriptMap, setExpandedTranscriptMap] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (feedPostsStatus !== "idle") {
      return;
    }
    void dispatch(fetchFeedPosts());
  }, [dispatch, feedPostsStatus]);

  return (
    <section className="screen-section feed-screen">
      <div className="feed-header">
        <h2>Feed</h2>
        <button className="btn btn-secondary btn-small" onClick={() => void dispatch(fetchFeedPosts())}>
          Refresh
        </button>
      </div>

      {feedPostsError && <div className="auth-error top-spaced">{feedPostsError}</div>}
      {feedReactionError && <div className="auth-error top-spaced">{feedReactionError}</div>}
      {feedPostsStatus === "loading" && feedPosts.length === 0 && <div className="empty-state">Loading feed posts...</div>}

      {feedPosts.length === 0 && feedPostsStatus !== "loading" ? (
        <div className="empty-state">Feed is empty. Publish a recording from Details to start a thread.</div>
      ) : (
        feedPosts.map((post) => (
          <div key={post.id} className="feed-card">
            <div className="feed-card-main">
              <div className="feed-card-topic">{post.topic}</div>
            </div>

            <div className="player feed-player">
              {post.audioDataUrl ? (
                <audio controls preload="metadata" src={post.audioDataUrl} className="feed-audio" />
              ) : (
                <div className="empty-state">Audio is unavailable for this post.</div>
              )}
              <div className="recording-duration">{formatTime(post.duration)}</div>
            </div>

            <div className="feed-post-actions">
              <button
                className="btn btn-secondary btn-small"
                onClick={() =>
                  setExpandedTranscriptMap((prev) => ({
                    ...prev,
                    [post.id]: !prev[post.id]
                  }))
                }
              >
                {expandedTranscriptMap[post.id] ? "Hide text" : "Show text"}
              </button>
              <button className="btn btn-secondary btn-small" onClick={() => dispatch(openFeedThread(post.id))}>
                Comments ({post.replyCount})
              </button>
            </div>

            <FeedReactionBar
              reactions={post.reactions}
              disabled={feedReactionStatus === "loading"}
              onReact={(reaction) => {
                void dispatch(reactToFeedPost({ postId: post.id, reaction }));
              }}
            />

            {expandedTranscriptMap[post.id] && (
              <div className="feed-transcript-accordion">
                <div className="section-title">Transcript</div>
                <div className="transcript-text">{post.transcript || "Transcript is unavailable."}</div>
              </div>
            )}
          </div>
        ))
      )}
    </section>
  );
}
