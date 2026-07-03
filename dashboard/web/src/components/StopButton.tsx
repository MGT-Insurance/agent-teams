import { stopSession } from "../lib/api.js";
import { useAsyncButtonState } from "../lib/useAsyncButtonState.js";

// StopButton — mirrors AttachButton's state machine but calls stopSession.
// Renders the button only (no layout wrapper); callers add wrappers as needed.
// On success the next snapshot tick removes the row (session gone) — no manual refetch.
export function StopButton({ initiativeId, sessionId }: { initiativeId: string; sessionId: string }) {
  const { state, trigger } = useAsyncButtonState();

  return (
    <button
      className="stop-btn"
      onClick={(e) => trigger(e, () => stopSession(initiativeId, sessionId))}
      disabled={state === "pending"}
      title="Stop session"
      aria-label="Stop session"
    >
      {state === "pending" ? "…" : state === "ok" ? "✓" : state === "err" ? "✗" : "■"}
    </button>
  );
}
