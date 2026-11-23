package verbs

import (
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// sendBridgeFromRun is the cloud-to-laptop hook for mail sent from a
// Dispatcher run. Two call sites reach it: a send to LocalStewardHandle
// (cause is nil), and a peer send whose recipient is not in the hosted
// workspace (cause is the bd.IsNotFound error from reading the recipient).
//
// Until the cloud-to-laptop track fills it in, it keeps the behavior from
// before the hook existed: the peer path's read-recipient error, or the peer
// path itself for a send to LocalStewardHandle.
func (c *sendKong) sendBridgeFromRun(ctx *cli.Context, cause error) error {
	if cause == nil {
		return c.sendPeerThroughOutbox(ctx)
	}
	return fmt.Errorf("ateam send: read recipient %s: %w; nothing sent", c.RecipientID, cause)
}
