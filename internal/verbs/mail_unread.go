// mail_unread.go holds the shared review-mail delivery seams frozen by
// CONTRACT agent-teams-8st0.18: unread lookup, dedup lookup, and the
// close-as-moot sequence. mail send (8st0.19), ateam close (8st0.20), the
// hung-scan backstop (8st0.21), route (8st0.27), and resume (8st0.28) all
// depend on these instead of re-implementing them.
package verbs

import (
	"errors"
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// errNothingToReview is returned by ateam resume (8st0.28) when a
// review-shaped initiative's PR probe finds MERGED or CLOSED — there is
// nothing left to review, and no worktree is (re)created. mail send
// (8st0.19) and the hung-scan backstop (8st0.21) both treat it as "wrap up
// cleanly", never as a delivery failure.
var errNothingToReview = errors.New("nothing to review: PR merged or closed")

// unreadMailFor returns id's unread messages. Thin wrapper over
// queryUnreadMessages (messaging.go) so callers outside Track M's messaging
// file — the hung-scan backstop, ateam close — depend on this shared
// contract file rather than reaching into messaging.go directly.
func unreadMailFor(ctx *cli.Context, id string) ([]bd.Issue, error) {
	return queryUnreadMessages(ctx, id)
}

// openMailWithDedupKey looks up an OPEN message assigned to id carrying
// label "dedup:<key>" (CONTRACT 8st0.18 item 1a). key == "" always misses —
// dedup is opt-in per send. Returns the existing message's id and true on a
// hit; "" and false on a miss (or an empty key).
func openMailWithDedupKey(ctx *cli.Context, id, key string) (string, bool, error) {
	if key == "" {
		return "", false, nil
	}
	var messages []bd.Issue
	if err := ctx.BD.RunJSON(&messages,
		"list",
		"--include-infra",
		"--assignee="+id,
		"--label=dedup:"+key,
		"--status=open",
		"--json",
	); err != nil {
		return "", false, fmt.Errorf("openMailWithDedupKey: query: %w", err)
	}
	messages = filterMessageType(messages)
	if len(messages) == 0 {
		return "", false, nil
	}
	return messages[0].ID, true, nil
}

// closeMailAs labels every message in ids with label, appends note, then
// force-closes it (bd close --force, same rationale as mail.go's
// mailCloseKong: a message bead closes unconditionally regardless of any
// pinned/gate state). Idempotent — bd close on an already-closed bead is a
// no-op (mirrors markMessageRead, messaging.go). Every step runs for every
// id regardless of an earlier step's failure on that id or a prior id's
// failure; each failure is reported to ctx.Stderr, and the first one
// encountered is returned so a caller can tell delivery wasn't fully clean —
// but a single stuck message bead never blocks closing the rest.
func closeMailAs(ctx *cli.Context, ids []string, label, note string) error {
	var firstErr error
	report := func(op, id string, err error) {
		fmt.Fprintf(ctx.Stderr, "closeMailAs: %s %s: %v\n", op, id, err)
		if firstErr == nil {
			firstErr = fmt.Errorf("closeMailAs: %s %s: %w", op, id, err)
		}
	}
	for _, id := range ids {
		if _, err := ctx.BD.Run("label", "add", id, label); err != nil {
			report("label", id, err)
		}
		if note != "" {
			if _, err := ctx.BD.Run("note", id, note); err != nil {
				report("note", id, err)
			}
		}
		if _, err := ctx.BD.Run("close", id, "--force"); err != nil {
			report("close", id, err)
		}
	}
	return firstErr
}
