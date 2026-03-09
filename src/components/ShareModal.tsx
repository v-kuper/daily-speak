"use client";

import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  closeShareModal,
  publishRecordingToFeed,
  setCopyMessage
} from "../store/slices/appSlice";

export default function ShareModal() {
  const dispatch = useAppDispatch();
  const { shareModalOpen, currentRecordingId, feedPublishStatus, feedPublishError } = useAppSelector((state) => state.app);

  if (!shareModalOpen) {
    return null;
  }

  const onConfirm = () => {
    if (!currentRecordingId) {
      dispatch(closeShareModal());
      return;
    }

    void dispatch(publishRecordingToFeed())
      .unwrap()
      .then(() => {
        window.setTimeout(() => {
          dispatch(setCopyMessage(null));
        }, 3500);
      })
      .catch(() => undefined);
  };

  return (
    <div
      className="modal visible"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) {
          if (feedPublishStatus === "loading") {
            return;
          }
          dispatch(closeShareModal());
        }
      }}
    >
      <div className="modal-content" role="dialog" aria-modal="true" aria-label="Publish recording">
        <div className="modal-title">Publish To Feed</div>
        <p>This will post this recording to the public feed for all signed-in users.</p>
        {feedPublishError && <div className="auth-error top-spaced">{feedPublishError}</div>}

        <div className="modal-buttons">
          <button
            className="btn btn-secondary"
            onClick={() => dispatch(closeShareModal())}
            disabled={feedPublishStatus === "loading"}
          >
            Cancel
          </button>
          <button className="btn btn-primary" onClick={onConfirm} disabled={feedPublishStatus === "loading"}>
            {feedPublishStatus === "loading" ? "Publishing..." : "Publish"}
          </button>
        </div>
      </div>
    </div>
  );
}
