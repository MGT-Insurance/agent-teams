// Route registration for the dashboard shell.
// Track B OWNS this file — tracks C/D/E must NOT edit it.
//
// View contract for each track:
//   Track C — InitiativesView: src/views/initiatives/index.tsx
//     default export: React component, no props, reads snapshot via useSnapshotContext()
//     navigation: call useNavigate() to push "/initiative/:id" on row click
//
//   Track D — InboxView: src/views/inbox/index.tsx
//     default export: React component, no props, reads snapshot via useSnapshotContext()
//
//   Track E — DrillInView: src/views/drillin/index.tsx
//     default export: React component, no props
//     initiative id: useParams<{ id: string }>() from react-router-dom
//     data: fetchInitiative(id) from src/lib/api.ts
//     logs: logsUrl(id, sessionId) from src/lib/api.ts → xterm.js
//     attach: attachToInitiative(id, sessionId) from src/lib/api.ts

import { Routes, Route, Navigate } from "react-router-dom";
import InboxView from "./views/inbox/index.js";
import InitiativesView from "./views/initiatives/index.js";
import DrillInView from "./views/drillin/index.js";

export function AppRoutes() {
  return (
    <Routes>
      {/* Default landing: Inbox (most actionable view) */}
      <Route path="/" element={<Navigate to="/inbox" replace />} />
      <Route path="/inbox" element={<InboxView />} />
      <Route path="/initiatives" element={<InitiativesView />} />
      {/* Old constellation path → Initiatives (its replacement). */}
      <Route path="/constellation" element={<Navigate to="/initiatives" replace />} />
      <Route path="/initiative/:id" element={<DrillInView />} />
      {/* Catch-all: redirect unknown paths to inbox */}
      <Route path="*" element={<Navigate to="/inbox" replace />} />
    </Routes>
  );
}
