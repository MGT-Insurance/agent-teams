import { useState } from "react";
import { launchSession } from "../lib/api.js";

type LaunchState = "idle" | "pending" | "ok" | "err";

// LaunchButton — mirrors StopButton's pattern (renders the button only, no
// layout wrapper), plus an inline error hint since launch failures are common
// enough to want a visible reason without a hover.
export function LaunchButton({ initiativeId }: { initiativeId: string }) {
  const [state, setState] = useState<LaunchState>("idle");
  const [errMsg, setErrMsg] = useState<string>("");

  const launch = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (state === "pending") return;
    setState("pending");
    setErrMsg("");
    try {
      await launchSession(initiativeId);
      setState("ok");
      setTimeout(() => setState("idle"), 3000);
    } catch (err) {
      setErrMsg(err instanceof Error ? err.message : String(err));
      setState("err");
      setTimeout(() => { setState("idle"); setErrMsg(""); }, 5000);
    }
  };

  const label = state === "idle" ? "▶" : state === "pending" ? "…" : state === "ok" ? "✓" : "✗";
  // In error state, set the title to the full error so it's inspectable on hover.
  const title =
    state === "err" && errMsg ? errMsg : "Launch a new DRI session for this initiative";
  // First line of error message — brief inline hint so the failure is legible at a glance.
  const errFirst = errMsg.split("\n")[0] ?? "";

  return (
    <>
      <button
        className={`launch-btn${state === "err" ? " launch-btn--err" : ""}`}
        onClick={(e) => { void launch(e); }}
        title={title}
        aria-label={state === "idle" ? "launch" : undefined}
      >
        {label}
      </button>
      {state === "err" && errFirst && (
        <span className="launch-btn__err-msg">{errFirst}</span>
      )}
    </>
  );
}
