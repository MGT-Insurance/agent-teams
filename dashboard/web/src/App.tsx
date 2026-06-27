import { NavLink } from "react-router-dom";
import { AppRoutes } from "./router.js";
import { useSnapshotContext } from "./SnapshotContext.js";

function ConnectionBadge() {
  const { connectionState, error } = useSnapshotContext();
  const label =
    connectionState === "connected" ? "live"
    : connectionState === "connecting" ? "connecting…"
    : connectionState === "reconnecting" ? "reconnecting…"
    : "error";
  return (
    <span className={`connection-badge connection-badge--${connectionState}`} title={error ?? undefined}>
      {label}
    </span>
  );
}

export function App() {
  return (
    <div className="app-shell">
      <nav className="nav">
        <span className="nav-title">agent-teams</span>
        <div className="nav-links">
          <NavLink to="/inbox" className={({ isActive }) => isActive ? "nav-link nav-link--active" : "nav-link"}>
            Inbox
          </NavLink>
          <NavLink to="/initiatives" className={({ isActive }) => isActive ? "nav-link nav-link--active" : "nav-link"}>
            Initiatives
          </NavLink>
        </div>
        <ConnectionBadge />
      </nav>
      <main className="view-area">
        <AppRoutes />
      </main>
    </div>
  );
}
