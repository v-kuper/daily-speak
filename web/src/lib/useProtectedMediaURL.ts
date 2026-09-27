"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { requestMediaPlaybackTicket } from "./mediaDownload";

type ProtectedMediaURL = {
  url: string | null;
  loading: boolean;
  error: string | null;
  reportReady: () => void;
  reportError: () => void;
  retry: () => void;
};

type ProtectedMediaState = {
  path: string | null;
  url: string | null;
  loading: boolean;
  error: string | null;
};

export const useProtectedMediaURL = (
  downloadPath: string | null,
  enabled = true,
): ProtectedMediaURL => {
  const playbackRetryAvailableRef = useRef(true);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<ProtectedMediaState>({
    path: null,
    url: null,
    loading: false,
    error: null,
  });
  const requestedPath = enabled ? downloadPath : null;

  useEffect(() => {
    playbackRetryAvailableRef.current = true;
  }, [downloadPath]);

  useEffect(() => {
    if (!requestedPath) {
      setState({ path: null, url: null, loading: false, error: null });
      return;
    }

    const controller = new AbortController();
    setState({ path: requestedPath, url: null, loading: true, error: null });
    void requestMediaPlaybackTicket({ downloadPath: requestedPath, signal: controller.signal })
      .then((ticket) => {
        if (!controller.signal.aborted) {
          setState({ path: requestedPath, url: ticket.url, loading: false, error: null });
        }
      })
      .catch((requestError: unknown) => {
        if (!controller.signal.aborted) {
          setState({
            path: requestedPath,
            url: null,
            loading: false,
            error: requestError instanceof Error
              ? requestError.message
              : "Protected media is temporarily unavailable.",
          });
        }
      });

    return () => controller.abort();
  }, [requestedPath, revision]);

  const reportReady = useCallback(() => {
    playbackRetryAvailableRef.current = true;
    setState((current) => current.path === requestedPath ? { ...current, error: null } : current);
  }, [requestedPath]);

  const reportError = useCallback(() => {
    setState((current) => current.path === requestedPath ? { ...current, url: null } : current);
    if (playbackRetryAvailableRef.current) {
      playbackRetryAvailableRef.current = false;
      setRevision((value) => value + 1);
      return;
    }
    setState((current) => current.path === requestedPath
      ? { ...current, loading: false, error: "Protected media could not be loaded. Please reload it." }
      : current);
  }, [requestedPath]);

  const retry = useCallback(() => {
    playbackRetryAvailableRef.current = true;
    setRevision((value) => value + 1);
  }, []);

  const currentState = state.path === requestedPath
    ? state
    : { path: requestedPath, url: null, loading: Boolean(requestedPath), error: null };

  return {
    url: currentState.url,
    loading: currentState.loading,
    error: currentState.error,
    reportReady,
    reportError,
    retry,
  };
};
