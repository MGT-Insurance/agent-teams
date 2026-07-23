// tie_session.go implements `ateam tie-session` — deterministic
// self-registration of a running session's id onto its initiative
// (agent-teams-zalv.3, at-ps11). This is the WRITER half of the
// session-to-initiative tie; see agent-teams-zalv.1 §7 for the resolved
// session-id-discovery mechanism this verb implements, and match.go for the
// helpers it calls (appendSessionID, resolveMyInitiative, isStewardSession).
package verbs

import (
	"errors"
	"fmt"
	"os"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// RegisterTieSessionKong registers the tie-session verb onto p.
func RegisterTieSessionKong(p *cli.Parser) {
	p.AddVerb("tie-session", "Tie this session's id to its initiative (self-registration).", &tieSessionKong{})
}

// tieSessionKong is the kong-native form of the tie-session verb. Both the
// initiative id and session id are optional — see Run for the fallback and
// no-op rules.
type tieSessionKong struct {
	InitiativeID string `arg:"" name:"initiative-id" optional:"" help:"Initiative ID to tie the session to (default: resolved from cwd)."`
	SessionID    string `name:"session-id" help:"Session id to tie (default: $CLAUDE_CODE_SESSION_ID)."`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// SILENT NO-OP contract (agent-teams-zalv.3): this verb is invoked on every
// SessionStart machine-wide via the SessionStart hook, so it must never
// surface output or fail a session start for the ordinary "nothing to do"
// cases:
//
//   - no session id available (neither --session-id nor
//     $CLAUDE_CODE_SESSION_ID is set, or the resolved value is the literal
//     "unknown" sentinel the hook scripts fall back to when stdin carries no
//     .session_id) -> exit 0, no output, no bd calls.
//   - cwd is the Steward's own session (isStewardSession) -> exit 0, no
//     output, no bd calls — the Steward is not an initiative bead.
//   - no initiative-id arg was given AND cwd resolves to no open initiative
//     -> exit 0, no output, no bd calls (covers non-initiative sessions and
//     spawned role agents in unregistered worktrees).
//
// The one exception is appendSessionID's one-open-initiative guard: a
// session id already tied to a DIFFERENT open initiative is a real conflict,
// not a silent second tie (Eric: "error/warn path, not a silent second
// tie"), so it prints a warning to stderr but still exits 0 — never breaks
// session start. Any other error out of appendSessionID (e.g. an
// explicitly-given initiative-id that doesn't exist) is a genuine failure
// and is returned as-is; the SessionStart hook wraps this call fail-soft, so
// a non-zero exit here still cannot block a session start.
func (c *tieSessionKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam tie-session: nil context")
	}

	sessionID := c.SessionID
	if sessionID == "" {
		sessionID = os.Getenv(sessionIDEnvVar)
	}
	// "unknown" is the sentinel session-start-inbox.sh (and its sibling hook
	// scripts) fall back to when stdin carries no .session_id — treat it
	// exactly like no session id at all so it's never written to the
	// registry as a garbage "session: unknown" line.
	if sessionID == "" || sessionID == "unknown" {
		return nil // no session id available — silent no-op
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil // can't resolve cwd — silent no-op, never break session start
	}
	if isStewardSession(ctx, cwd) {
		return nil // the Steward is not an initiative bead
	}

	initiativeID := c.InitiativeID
	if initiativeID == "" {
		initiativeID, err = resolveMyInitiative(ctx, cwd)
		if err != nil {
			return nil // no open initiative registered for cwd — silent no-op
		}
	}

	if err := appendSessionID(ctx, initiativeID, sessionID); err != nil {
		if errors.Is(err, errSessionTiedElsewhere) {
			fmt.Fprintf(ctx.Stderr, "ateam tie-session: %v\n", err)
			return nil
		}
		return fmt.Errorf("ateam tie-session: %w", err)
	}

	fmt.Fprintf(ctx.Stdout, "tie-session: tied session %s to %s\n", sessionID, initiativeID)
	return nil
}
