import { useState } from "react";
import { attachToInitiative } from "../lib/api.js";

// AttachButton — mirrors StopButton's state-machine pattern (renders the
// button only, no layout wrapper). Component extraction only — the internal
// state machine is not unified with StopButton/LaunchButton here; that is a
// later enhancement-ring bead.
export function AttachButton({ initiativeId, sessionId }: { initiativeId: string; sessionId: string }) {
  const [state, setState] = useState<"idle" | "pending" | "ok" | "err">("idle");

  async function handleClick(e: React.MouseEvent<HTMLButtonElement>) {
    e.stopPropagation();
    if (state === "pending") return;
    setState("pending");
    try {
      await attachToInitiative(initiativeId, sessionId);
      setState("ok");
      setTimeout(() => setState("idle"), 1500);
    } catch {
      setState("err");
      setTimeout(() => setState("idle"), 3000);
    }
  }

  return (
    <button
      className="attach-btn attach-btn--row"
      onClick={(e) => { void handleClick(e); }}
      disabled={state === "pending"}
      title="Attach to session"
      aria-label="Attach to session"
    >
      {state === "pending" ? "…" : state === "ok" ? "✓" : state === "err" ? "✗" : "↗"}
    </button>
  );
}
