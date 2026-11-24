package verbs

import (
	"errors"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// sendToCloudSteward is the laptop-to-cloud-steward hook of send. It is a stub
// until the cloud-steward send bead replaces it.
func (c *sendKong) sendToCloudSteward(ctx *cli.Context, sender string) error {
	return errors.New("ateam send: cloud-steward mail is not built yet; nothing sent")
}
