// This file is owned by the at-wolk LOOP Track B (agent-teams-e3mq.3). It
// wires the gate verb's notify hook to the Steward instead of the human
// phone transport, superseding agent-teams-tlx7's direct gate->phone route.
package verbs

import (
	"fmt"
	"os"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// notifyToSteward is a gateNotifyFunc (kong_converted.go) that routes a
// gate's ask to the Steward instead of the human phone transport used by
// notifyForGate (notify.go, left in place and still referenced by nothing —
// legal Go, and Track D's notify.go stays untouched).
//
// gateNotifyFunc's signature (ctx, id, file) carries no gate-kind field, so
// kind is derived from the "gate:review"/"gate:question" label the calling
// gateKong.Run already wrote to the bead before invoking notify (see
// query.go's gateKind, the same source humanListKong reads).
//
// The ask body at file is wrapped in a Gate->Steward envelope
// (BuildStewardGateEnvelope, steward_seams.go) and handed to the Steward's
// mailbox via an in-process sendKong.Run — the same DI wiring
// mail_register.go uses for `ateam mail send` — rather than exec'ing the
// ateam binary. Best-effort: same non-fatal semantics as notifyForGate: the
// caller (gateKong.Run) already treats a non-nil return here as a
// warn-and-continue, never a gate failure.
func notifyToSteward(ctx *cli.Context, id, file string) error {
	body, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("notify to steward: read file: %w", err)
	}

	issue, err := bd.ShowIssue(ctx.BD, id)
	if err != nil {
		return fmt.Errorf("notify to steward: look up initiative %s: %w", id, err)
	}
	kind := StewardGateKindQuestion
	if gateKind(issue.Labels) == "REVIEW" {
		kind = StewardGateKindReview
	}

	envelope, err := BuildStewardGateEnvelope(id, kind, string(body))
	if err != nil {
		return fmt.Errorf("notify to steward: build envelope: %w", err)
	}

	tmp, err := os.CreateTemp("", "ateam-steward-gate-*")
	if err != nil {
		return fmt.Errorf("notify to steward: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(envelope); err != nil {
		tmp.Close()
		return fmt.Errorf("notify to steward: write temp file: %w", err)
	}
	tmp.Close()

	send := &sendKong{
		RecipientID:    StewardHandle,
		File:           tmpPath,
		Sender:         "gate",
		agentsFunc:     defaultAgentsJSONAll,
		resumeFunc:     defaultResume,
		sleeper:        defaultSleeper,
		doorbellExists: defaultDoorbellExists,
		respawnFunc:    defaultRespawn,
	}
	return send.Run(ctx)
}
