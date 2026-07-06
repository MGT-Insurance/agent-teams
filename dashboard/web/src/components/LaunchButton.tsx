import { launchSession } from "../lib/api.js";
import { useAsyncButtonState } from "../lib/useAsyncButtonState.js";

// LaunchButton — mirrors StopButton/AttachButton's pattern (renders the
// button only, no layout wrapper), plus an inline error hint since launch
// failures are common enough to want a visible reason without a hover.
export function LaunchButton({ initiativeId }: { initiativeId: string }) {
  const { state, errMsg, trigger } = useAsyncButtonState({ okTimeoutMs: 3000, errTimeoutMs: 5000 });

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
        onClick={(e) => trigger(e, () => launchSession(initiativeId))}
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
