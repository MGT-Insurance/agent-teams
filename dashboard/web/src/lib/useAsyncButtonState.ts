import { useRef, useState } from "react";

// Shared idle -> pending -> ok/err lifecycle for StopButton/LaunchButton/
// AttachButton. Each button used to duplicate this state machine (with its
// own timeout-reset and stopPropagation) — this hook is the single copy.
export type AsyncButtonState = "idle" | "pending" | "ok" | "err";

export interface UseAsyncButtonStateOptions {
  // Delay before an ok/err state auto-resets to idle. Defaults match
  // StopButton/AttachButton's original 1500ms/3000ms.
  okTimeoutMs?: number;
  errTimeoutMs?: number;
}

export interface UseAsyncButtonStateResult {
  state: AsyncButtonState;
  // First-class error message from the last failed action (empty otherwise).
  // Buttons that don't render it (Stop/Attach) can simply ignore it.
  errMsg: string;
  // Click handler glue: stops propagation, guards re-entrant clicks while
  // pending, runs `action`, and drives the state transitions above.
  trigger: (e: React.MouseEvent, action: () => Promise<unknown>) => void;
}

const DEFAULT_OK_TIMEOUT_MS = 1500;
const DEFAULT_ERR_TIMEOUT_MS = 3000;

export function useAsyncButtonState(
  options: UseAsyncButtonStateOptions = {}
): UseAsyncButtonStateResult {
  const { okTimeoutMs = DEFAULT_OK_TIMEOUT_MS, errTimeoutMs = DEFAULT_ERR_TIMEOUT_MS } = options;
  const [state, setState] = useState<AsyncButtonState>("idle");
  const [errMsg, setErrMsg] = useState("");
  // Ref mirrors `state` so the pending-guard reads the latest value even
  // though `trigger` is recreated fresh (unmemoized) on every render.
  const stateRef = useRef<AsyncButtonState>(state);
  stateRef.current = state;

  function trigger(e: React.MouseEvent, action: () => Promise<unknown>) {
    e.stopPropagation();
    if (stateRef.current === "pending") return;
    setState("pending");
    setErrMsg("");
    void (async () => {
      try {
        await action();
        setState("ok");
        setTimeout(() => setState("idle"), okTimeoutMs);
      } catch (err) {
        setErrMsg(err instanceof Error ? err.message : String(err));
        setState("err");
        setTimeout(() => {
          setState("idle");
          setErrMsg("");
        }, errTimeoutMs);
      }
    })();
  }

  return { state, errMsg, trigger };
}
