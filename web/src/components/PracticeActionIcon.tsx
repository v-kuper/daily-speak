type PracticeAction = "play" | "stop" | "microphone" | "microphone-off" | "retry";

export default function PracticeActionIcon({ action }: { action: PracticeAction }) {
  return (
    <svg className="practice-action-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
      {action === "play" && <path d="m8 5 11 7-11 7V5Z" />}
      {action === "stop" && <rect x="6" y="6" width="12" height="12" rx="2" />}
      {action === "microphone" && <>
        <rect x="9" y="3" width="6" height="11" rx="3" />
        <path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3m-4 0h8" />
      </>}
      {action === "microphone-off" && <>
        <path d="M9 5a3 3 0 0 1 6 1v3M9 9v2a3 3 0 0 0 5 2M5 10v2a7 7 0 0 0 12 5M19 10v2a7 7 0 0 1-.5 2.6M12 19v3m-4 0h8M3 3l18 18" />
      </>}
      {action === "retry" && <>
        <path d="M20 7V3h-4M20 3l-3 3a8 8 0 1 0 3 9" />
      </>}
    </svg>
  );
}
