import { useEffect, useReducer, useRef } from "react";
import type { SnapshotEvent } from "@agent-teams/shared";
import { API_PATHS } from "@agent-teams/shared";

export type ConnectionState = "connecting" | "connected" | "reconnecting" | "error";

export interface SnapshotState {
  initiatives: SnapshotEvent["initiatives"];
  unmatchedSessions: SnapshotEvent["unmatchedSessions"];
  inbox: SnapshotEvent["inbox"];
  ts: number | null;
  connectionState: ConnectionState;
  error: string | null;
}

type Action =
  | { type: "connecting" }
  | { type: "snapshot"; payload: SnapshotEvent }
  | { type: "reconnecting" }
  | { type: "error"; message: string };

const initialState: SnapshotState = {
  initiatives: [],
  unmatchedSessions: [],
  inbox: [],
  ts: null,
  connectionState: "connecting",
  error: null,
};

export function snapshotReducer(state: SnapshotState, action: Action): SnapshotState {
  switch (action.type) {
    case "connecting":
      return { ...state, connectionState: "connecting", error: null };
    case "snapshot":
      return {
        ...state,
        initiatives: action.payload.initiatives,
        unmatchedSessions: action.payload.unmatchedSessions ?? [],
        inbox: action.payload.inbox,
        ts: action.payload.ts,
        connectionState: "connected",
        error: null,
      };
    case "reconnecting":
      return { ...state, connectionState: "reconnecting" };
    case "error":
      return { ...state, connectionState: "error", error: action.message };
  }
}

// Parses a raw SSE `data:` string into a SnapshotEvent.
// Returns null (and logs) if the payload is malformed — never throws.
export function parseSSEPayload(raw: string): SnapshotEvent | null {
  try {
    const parsed: unknown = JSON.parse(raw);
    if (
      parsed !== null &&
      typeof parsed === "object" &&
      "initiatives" in parsed &&
      "inbox" in parsed &&
      "ts" in parsed &&
      Array.isArray((parsed as Record<string, unknown>)["initiatives"]) &&
      Array.isArray((parsed as Record<string, unknown>)["inbox"])
    ) {
      return parsed as SnapshotEvent;
    }
    return null;
  } catch {
    return null;
  }
}

// Base delay before first reconnect; doubles on each attempt (capped at 30s).
const RECONNECT_BASE_MS = 1_000;
const RECONNECT_MAX_MS = 30_000;

export function useSnapshot(): SnapshotState {
  const [state, dispatch] = useReducer(snapshotReducer, initialState);
  const attemptRef = useRef(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let cancelled = false;

    function connect() {
      if (cancelled) return;
      dispatch({ type: "connecting" });

      const es = new EventSource(API_PATHS.events);
      esRef.current = es;

      es.addEventListener("message", (evt) => {
        const payload = parseSSEPayload(evt.data as string);
        if (payload) {
          attemptRef.current = 0;
          dispatch({ type: "snapshot", payload });
        } else {
          dispatch({ type: "error", message: "Received malformed snapshot payload" });
        }
      });

      // The SSE spec uses the "snapshot" event type per the contract.
      es.addEventListener("snapshot", (evt) => {
        const payload = parseSSEPayload((evt as MessageEvent).data as string);
        if (payload) {
          attemptRef.current = 0;
          dispatch({ type: "snapshot", payload });
        } else {
          dispatch({ type: "error", message: "Received malformed snapshot payload" });
        }
      });

      es.onerror = () => {
        es.close();
        if (cancelled) return;
        dispatch({ type: "reconnecting" });
        const delay = Math.min(RECONNECT_BASE_MS * 2 ** attemptRef.current, RECONNECT_MAX_MS);
        attemptRef.current += 1;
        timerRef.current = setTimeout(connect, delay);
      };
    }

    connect();

    return () => {
      cancelled = true;
      esRef.current?.close();
      if (timerRef.current !== null) clearTimeout(timerRef.current);
    };
  }, []);

  return state;
}
