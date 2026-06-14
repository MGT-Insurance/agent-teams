// Track E owns this file. Contract: default-export a React component.
// Props: none — the initiative :id comes from useParams() (react-router-dom).
//   import { useParams } from "react-router-dom";
//   const { id } = useParams<{ id: string }>();
// Fetch full detail with fetchInitiative(id) from src/lib/api.ts.
// Logs: stream via logsUrl(id, sessionId) piped into xterm.js (raw ANSI — do NOT strip).
// Attach: call attachToInitiative(id, sessionId) from src/lib/api.ts.
//
// Replace this stub with the real Drill-in view.
export default function DrillInView() {
  return <div className="view-placeholder">Drill-in — Track E coming</div>;
}
