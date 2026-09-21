"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { normalizeTickerOffset } from "../lib/interviewGuidance";

type GuidanceWordTickerProps = {
  words: string[];
};

const TICKER_SPEED_PX_PER_SECOND = 18;

export default function GuidanceWordTicker({ words }: GuidanceWordTickerProps) {
  const normalizedWords = useMemo(() => {
    const seen = new Set<string>();
    return words
      .map((word) => word.trim().replace(/\s+/g, " "))
      .filter((word) => {
        const key = word.toLocaleLowerCase();
        if (!word || seen.has(key)) {
          return false;
        }
        seen.add(key);
        return true;
      });
  }, [words]);
  const trackRef = useRef<HTMLDivElement | null>(null);
  const frameRef = useRef<number | null>(null);
  const offsetRef = useRef(0);
  const lastFrameTimeRef = useRef<number | null>(null);
  const dragStartXRef = useRef(0);
  const dragStartOffsetRef = useRef(0);
  const draggingRef = useRef(false);
  const keyboardPausedRef = useRef(false);
  const reducedMotionRef = useRef(false);
  const [dragging, setDragging] = useState(false);
  const [keyboardPaused, setKeyboardPaused] = useState(false);
  const [reducedMotion, setReducedMotion] = useState(false);

  const cycleWidth = useCallback(() => {
    return trackRef.current?.firstElementChild?.getBoundingClientRect().width ?? 0;
  }, []);

  const paintOffset = useCallback((offset: number) => {
    const width = cycleWidth();
    offsetRef.current = normalizeTickerOffset(offset, width);
    if (trackRef.current) {
      trackRef.current.style.transform = `translate3d(${-offsetRef.current}px, 0, 0)`;
    }
  }, [cycleWidth]);

  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    const applyPreference = () => {
      reducedMotionRef.current = mediaQuery.matches;
      setReducedMotion(mediaQuery.matches);
    };
    applyPreference();
    mediaQuery.addEventListener("change", applyPreference);
    return () => mediaQuery.removeEventListener("change", applyPreference);
  }, []);

  useEffect(() => {
    const animate = (time: number) => {
      const previousTime = lastFrameTimeRef.current ?? time;
      const elapsed = Math.min(50, Math.max(0, time - previousTime));
      lastFrameTimeRef.current = time;
      if (!draggingRef.current && !keyboardPausedRef.current && !reducedMotionRef.current) {
        paintOffset(offsetRef.current + (TICKER_SPEED_PX_PER_SECOND * elapsed) / 1000);
      }
      frameRef.current = window.requestAnimationFrame(animate);
    };

    lastFrameTimeRef.current = null;
    frameRef.current = window.requestAnimationFrame(animate);
    return () => {
      if (frameRef.current !== null) {
        window.cancelAnimationFrame(frameRef.current);
      }
      frameRef.current = null;
    };
  }, [normalizedWords, paintOffset]);

  useEffect(() => {
    offsetRef.current = 0;
    paintOffset(0);
  }, [normalizedWords, paintOffset]);

  if (normalizedWords.length === 0) {
    return null;
  }

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return;
    }
    draggingRef.current = true;
    setDragging(true);
    dragStartXRef.current = event.clientX;
    dragStartOffsetRef.current = offsetRef.current;
    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!draggingRef.current) {
      return;
    }
    event.preventDefault();
    paintOffset(dragStartOffsetRef.current - (event.clientX - dragStartXRef.current));
  };

  const releasePointer = (event: PointerEvent<HTMLDivElement>) => {
    if (!draggingRef.current) {
      return;
    }
    draggingRef.current = false;
    setDragging(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === " ") {
      event.preventDefault();
      keyboardPausedRef.current = !keyboardPausedRef.current;
      setKeyboardPaused(keyboardPausedRef.current);
      return;
    }
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
      paintOffset(offsetRef.current + (event.key === "ArrowRight" ? 48 : -48));
    }
  };

  const status = reducedMotion
    ? "Manual scrolling"
    : dragging
      ? "Paused"
      : keyboardPaused
        ? "Paused — press Space to resume"
        : "Hold or drag";

  return (
    <div className="guidance-word-ticker">
      <div className="guidance-word-ticker-header">
        <span>Useful words</span>
        <span>{status}</span>
      </div>
      <div
        className={`guidance-word-ticker-viewport ${dragging ? "dragging" : ""}`}
        role="region"
        aria-label="Useful words. Hold or drag to move the list. Press Space to pause or resume."
        tabIndex={0}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={releasePointer}
        onPointerCancel={releasePointer}
        onLostPointerCapture={releasePointer}
        onKeyDown={onKeyDown}
      >
        <div ref={trackRef} className="guidance-word-ticker-track">
          {[0, 1].map((copyIndex) => (
            <div key={copyIndex} className="guidance-word-ticker-copy" aria-hidden={copyIndex === 1}>
              {normalizedWords.map((word) => (
                <span key={`${copyIndex}-${word.toLocaleLowerCase()}`} className="guidance-word-ticker-chip">
                  {word}
                </span>
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
