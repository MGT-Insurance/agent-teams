import { createContext, useContext, type ReactNode } from "react";
import { useSnapshot, type SnapshotState } from "./hooks/useSnapshot.js";

const SnapshotContext = createContext<SnapshotState | null>(null);

export function SnapshotProvider({ children }: { children: ReactNode }) {
  const snapshot = useSnapshot();
  return (
    <SnapshotContext.Provider value={snapshot}>
      {children}
    </SnapshotContext.Provider>
  );
}

// All three views use this to access the live snapshot without opening their own SSE stream.
export function useSnapshotContext(): SnapshotState {
  const ctx = useContext(SnapshotContext);
  if (!ctx) throw new Error("useSnapshotContext must be used inside SnapshotProvider");
  return ctx;
}
