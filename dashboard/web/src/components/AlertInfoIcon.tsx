import type { Alert } from "@agent-teams/shared";
import "./AlertInfoIcon.css";

// Shared "i" info-icon + hover/keyboard-focus popover for a row's alert
// Why/Do detail (agent-teams-m380.1). Used by both Initiatives and Inbox
// rows. The outer slot always renders (fixed-size, position:absolute
// bottom-right of the row) so the layout stays stable across alerted and
// non-alerted rows; the icon + popover render only when alert is present.
// Click/keydown stopPropagation on the icon keeps interacting with it from
// firing the row's navigate-away handler.
export function AlertInfoIcon({ alert }: { alert: Alert | null }) {
  return (
    <div className="alert-info" data-tier={alert?.level}>
      {alert && (
        <>
          <span
            className="alert-info__icon"
            tabIndex={0}
            aria-label={`Why: ${alert.reason} Do: ${alert.action}`}
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => e.stopPropagation()}
          >
            i
          </span>
          <span className="alert-info__pop" role="tooltip">
            <span className="alert-info__why">
              <span className="alert-info__label">Why:</span> {alert.reason}
            </span>
            <span className="alert-info__do">
              <span className="alert-info__label">Do:</span> {alert.action}
            </span>
          </span>
        </>
      )}
    </div>
  );
}
