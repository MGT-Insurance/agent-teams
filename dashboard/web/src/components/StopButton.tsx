import { useState } from "react";
import { stopSession } from "../lib/api.js";

// StopButton — mirrors RowAttachButton's state machine but calls stopSession.
// Renders the button only (no layout wrapper); callers add wrappers as needed.
// On success the next snapshot tick removes the row (session gone) — no manual refetch.
export function StopButton({ initiativeId, sessionId }: { initiativeId: string; sessionId: string }) {
  const [state, setState] = useState<"idle" | "pending" | "ok" | "err">("idle");

  async function handleClick(e: React.MouseEvent<HTMLButtonElement>) {
    e.stopPropagation();
    if (state === "pending") return;
    setState("pending");
    try {
      await stopSession(initiativeId, sessionId);
      setState("ok");
      setTimeout(() => setState("idle"), 1500);
    } catch {
      setState("err");
      setTimeout(() => setState("idle"), 3000);
    }
  }

  return (
    <button
      className="stop-btn"
      onClick={(e) => { void handleClick(e); }}
      disabled={state === "pending"}
      title="Stop session"
      aria-label="Stop session"
    >
      {state === "pending" ? "…" : state === "ok" ? "✓" : state === "err" ? "✗" : "stop"}
    </button>
  );
}
