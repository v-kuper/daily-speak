"use client";

import { buildShareLink } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  closeShareModal,
  openSharePreview,
  setCopyMessage,
  setShareAction
} from "../store/slices/appSlice";

export default function ShareModal() {
  const dispatch = useAppDispatch();
  const { shareModalOpen, shareAction, currentRecordingId } = useAppSelector((state) => state.app);

  if (!shareModalOpen) {
    return null;
  }

  const onConfirm = async () => {
    if (!currentRecordingId) {
      dispatch(closeShareModal());
      return;
    }

    if (shareAction === "preview") {
      dispatch(openSharePreview());
      return;
    }

    const link = buildShareLink(currentRecordingId);

    try {
      await navigator.clipboard.writeText(link);
    } catch {
      // Clipboard might be unavailable in restricted contexts.
    }

    dispatch(closeShareModal());
    dispatch(setCopyMessage(`Link copied: ${link}`));

    window.setTimeout(() => {
      dispatch(setCopyMessage(null));
    }, 3500);
  };

  return (
    <div
      className="modal visible"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) {
          dispatch(closeShareModal());
        }
      }}
    >
      <div className="modal-content" role="dialog" aria-modal="true" aria-label="Share recording">
        <div className="modal-title">Share Recording</div>

        <label className="share-option">
          <input
            type="radio"
            name="share-action"
            checked={shareAction === "copy"}
            onChange={() => dispatch(setShareAction("copy"))}
          />
          <span>Copy link</span>
        </label>

        <label className="share-option">
          <input
            type="radio"
            name="share-action"
            checked={shareAction === "preview"}
            onChange={() => dispatch(setShareAction("preview"))}
          />
          <span>Open share preview</span>
        </label>

        <div className="modal-buttons">
          <button className="btn btn-secondary" onClick={() => dispatch(closeShareModal())}>
            Cancel
          </button>
          <button className="btn btn-primary" onClick={() => void onConfirm()}>
            Confirm
          </button>
        </div>
      </div>
    </div>
  );
}
