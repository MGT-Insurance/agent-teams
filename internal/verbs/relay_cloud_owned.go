package verbs

// REMOVAL CONDITION: delete this file, the relayKong cloudOwned field, and
// its call site in handleReply once the laptop's workspace home IS the hosted
// workspace (laptops read hosted). From then on the normal thread label
// lookup finds cloud topics directly, and this check would wrongly skip
// local ones.
//
// Why it exists: one Telegram forum group is shared by the laptop relay and
// the Dispatcher bot. Cloud initiatives live in the hosted workspace
// (cloud_home), so the laptop relay finds no thread:<n> bead for a cloud
// topic and would forward the reply to the local Steward as unrouted. The
// Dispatcher labels each cloud topic's initiative thread:<n> in the hosted
// workspace; this check asks the hosted workspace and lets handleReply skip
// those replies.

import (
	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// relayCloudOwnedFunc reports whether the hosted workspace holds an
// initiative (open or closed) carrying label. Injected so tests can
// substitute a fake.
type relayCloudOwnedFunc func(label string) (owned bool, err error)

// defaultCloudOwned returns a relayCloudOwnedFunc that queries the hosted
// workspace named by cloud_home in the config under home. When cloud_home is
// not configured, it always reports (false, nil).
func defaultCloudOwned(home string) relayCloudOwnedFunc {
	return func(label string) (bool, error) {
		cloudHome, ok, err := workspaceconfig.CloudHome(home)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		return cloudLabelExists(defaultCloudBD(cloudHome), label)
	}
}

// cloudLabelExists runs `bd list --all --label=<label> --json --limit 1`
// through client and reports whether at least one issue came back.
func cloudLabelExists(client cli.BDRunner, label string) (bool, error) {
	var issues []bd.Issue
	if err := client.RunJSON(&issues, "list", "--all", "--label="+label, "--json", "--limit", "1"); err != nil {
		return false, err
	}
	return len(issues) > 0, nil
}
