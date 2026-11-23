package verbs

import "github.com/mgt-insurance/agent-teams/internal/cli"

// trySendToCloud is the laptop-to-cloud hook in sendKong.Run. It reports
// handled=true when it delivered (or refused) the message itself, so Run
// returns err. Until the laptop-to-cloud track fills it in, it handles nothing
// and makes no bd call.
func (c *sendKong) trySendToCloud(ctx *cli.Context, sender string) (handled bool, err error) {
	return false, nil
}
