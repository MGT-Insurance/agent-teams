import { attachToInitiative } from "../lib/api.js";
import { useAsyncButtonState } from "../lib/useAsyncButtonState.js";

// AttachButton — mirrors StopButton's state-machine pattern (renders the
// button only, no layout wrapper).
export function AttachButton({ initiativeId, sessionId }: { initiativeId: string; sessionId: string }) {
  const { state, trigger } = useAsyncButtonState();

  return (
    <button
      className="attach-btn attach-btn--row"
      onClick={(e) => trigger(e, () => attachToInitiative(initiativeId, sessionId))}
      disabled={state === "pending"}
      title="Attach to session"
      aria-label="Attach to session"
    >
      {state === "pending" ? "…" : state === "ok" ? "✓" : state === "err" ? "✗" : "↗"}
    </button>
  );
}
