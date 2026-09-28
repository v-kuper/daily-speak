"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { buildTranscriptSegments } from "../lib/transcriptHighlight";
import ConversationTranscript from "./ConversationTranscript";
import {
  fetchGuestPreview,
  guestPreviewPath,
  type GuestPreview,
} from "../lib/guestPreview";
import { formatTime } from "../lib/utils";
import SuggestionCard from "./SuggestionCard";

const statusLabel = (preview: GuestPreview): string => {
  if (preview.state === "queued") return "Your preview is waiting for an analysis slot...";
  if (preview.state === "processing") return "Transcribing and finding the clearest improvements...";
  return "Your guest preview is ready.";
};

export default function GuestPreviewScreen({ previewId }: { previewId: string }) {
  const [preview, setPreview] = useState<GuestPreview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [retryKey, setRetryKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const load = async () => {
      try {
        const next = await fetchGuestPreview(previewId);
        if (cancelled) return;
        setPreview(next);
        setError(null);
        if (next.state === "queued" || next.state === "processing") {
          timer = setTimeout(load, 3000);
        }
      } catch (loadError) {
        if (!cancelled) {
          setError(loadError instanceof Error ? loadError.message : "Cannot load the guest preview.");
        }
      }
    };

    void load();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [previewId, retryKey]);

  const transcriptSegments = useMemo(
    () => buildTranscriptSegments(preview?.transcript ?? "", preview?.corrections ?? []),
    [preview?.corrections, preview?.transcript]
  );
  const authPath = `/auth?returnTo=${encodeURIComponent(guestPreviewPath(previewId))}`;

  if (!preview && error) {
    return (
      <section>
        <Link className="back-btn" href="/speak">← Back to speaking</Link>
        <h2>Guest preview</h2>
        <div className="auth-error">{error}</div>
        <div className="auth-buttons top-spaced">
          <button className="btn btn-secondary" onClick={() => setRetryKey((value) => value + 1)}>Retry</button>
          <Link className="btn btn-primary" href={authPath}>Sign in to continue</Link>
        </div>
      </section>
    );
  }

  if (!preview) {
    return (
      <section>
        <Link className="back-btn" href="/speak">← Back to speaking</Link>
        <h2>Guest preview</h2>
        <div className="notice" role="status">
          Loading your recording...
          <div className="background-progress" aria-hidden="true"><div className="background-progress-fill" /></div>
        </div>
      </section>
    );
  }

  const processing = preview.state === "queued" || preview.state === "processing";
  const ready = preview.state === "ready";
  const hasConversationTranscript = preview.practiceType === "topic" && preview.interviewTurns.length > 0;

  return (
    <section>
      <Link className="back-btn" href="/speak">← Back to speaking</Link>
      <div className="guest-preview-heading">
        <div>
          <div className="heading-sm">Guest result</div>
          <h2>Your speaking preview</h2>
        </div>
        <span className="guest-preview-badge">Limited</span>
      </div>

      <div className="details-metadata">
        <div><strong>{formatTime(preview.duration)}</strong></div>
        <div>{preview.topic}</div>
        <div>{preview.practiceType === "free_talk" ? "Free talk" : "Topic"}</div>
      </div>

      {processing && (
        <div className="notice" role="status">
          {statusLabel(preview)}
          <div className="background-progress" aria-hidden="true"><div className="background-progress-fill" /></div>
        </div>
      )}
      {preview.state === "failed" && (
        <div className="auth-error">{preview.processingError ?? "The guest analysis failed. Please record another sample."}</div>
      )}
      {error && (
        <div className="auth-error">
          {error}
          <button className="btn btn-secondary btn-small top-spaced" onClick={() => setRetryKey((value) => value + 1)}>
            Retry update
          </button>
        </div>
      )}

      <div className="transcript-section">
        <div className="section-title">{hasConversationTranscript ? "Conversation transcript" : "Transcript"}</div>
        {hasConversationTranscript ? (
          <ConversationTranscript
            turns={preview.interviewTurns}
            suggestions={preview.corrections}
            processing={processing}
          />
        ) : preview.transcript ? (
          <div className="transcript-text">
            {transcriptSegments.map((segment, index) => segment.isError ? (
              <mark
                key={`guest-transcript-${index}`}
                className={`transcript-error-mark${segment.severity ? ` transcript-error-mark-${segment.severity}` : ""}`}
              >
                {segment.text}
              </mark>
            ) : <span key={`guest-transcript-${index}`}>{segment.text}</span>)}
          </div>
        ) : (
          <div className="empty-state">{processing ? "Your transcript will appear here automatically." : "Transcript is unavailable."}</div>
        )}
      </div>

      <div className="suggestions-section">
        <div className="section-title">Preview corrections</div>
        {preview.corrections.length > 0 ? preview.corrections.map((suggestion, index) => (
          <SuggestionCard key={`${suggestion.wrong}-${suggestion.right}-${index}`} suggestion={suggestion} />
        )) : (
          <div className="empty-state">
            {processing ? "We are selecting up to two high-confidence corrections." : ready ? "No clear correction was needed in this preview." : "Corrections are unavailable."}
          </div>
        )}
      </div>

      <div className="guest-unlock-card">
        <div className="section-title">Full personal analysis</div>
        <div className="guest-locked-content" aria-hidden="true">
          <div className="guest-locked-line guest-locked-line-wide" />
          <div className="guest-locked-line" />
          <div className="guest-locked-line guest-locked-line-short" />
        </div>
        <div className="guest-unlock-copy">
          Sign in to unlock the complete error list, a natural rewritten version, and shadowing audio. This preview will become your first saved recording.
        </div>
        <Link className="btn btn-primary btn-large" href={authPath}>
          Sign in to unlock full analysis
        </Link>
        {preview.state === "failed" && <div className="hint">Guest access includes one preview. Sign in to record another sample.</div>}
      </div>
    </section>
  );
}
