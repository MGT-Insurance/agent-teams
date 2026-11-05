package verbs

// REMOVAL CONDITION: delete this file, the relayKong cloudOwned field, and
// its call site in handleReply once the laptop's workspace home IS the hosted
// workspace (laptops read hosted). From then on the normal thread label
// lookup finds cloud topics directly, and this check would wrongly skip
// local ones. defaultCloudOwned already reports (false, nil) when cloud_home
// resolves to the laptop home, so a stale copy of this code stays inert.
//
// Why it exists: one Telegram forum group is shared by the laptop relay and
// the Dispatcher bot. Cloud initiatives live in the hosted workspace
// (cloud_home), so the laptop relay finds no thread:<n> bead for a cloud
// topic and would forward the reply to the local Steward as unrouted. The
// Dispatcher labels each cloud topic's initiative thread:<n> in the hosted
// workspace; this check asks the hosted workspace and lets handleReply skip
// those replies.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// cloudOwnedTimeout bounds the hosted label query. Receive handles replies
// serially, so an unbounded query against an unreachable hosted server would
// stall getUpdates; a timeout surfaces as an error and handleReply continues
// down the unrouted path. The hosted lookup takes about 2.8s per call
// (measured November 5, 2026), so the bound leaves wide margin: a timeout
// on a healthy server would misroute the reply this check exists to prevent.
const cloudOwnedTimeout = 10 * time.Second

// relayCloudOwnedFunc reports whether the hosted workspace holds an
// initiative (open or closed) carrying label. Injected so tests can
// substitute a fake.
type relayCloudOwnedFunc func(label string) (owned bool, err error)

// cloudRunFunc runs one bd command against the hosted workspace, bounded by
// ctx, and returns its stdout. (*bd.Client).RunContext satisfies it.
type cloudRunFunc func(ctx context.Context, args ...string) (string, error)

// sameWorkspace reports whether a and b resolve to the same directory, after
// making each absolute and following symlinks.
func sameWorkspace(a, b string) bool {
	norm := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Clean(p)
	}
	return norm(a) == norm(b)
}

// defaultCloudOwned returns a relayCloudOwnedFunc that queries the hosted
// workspace named by cloud_home in the config under home. When cloud_home is
// not configured, or resolves to the same directory as home, it always
// reports (false, nil). In the same-directory case the laptop already reads
// the hosted workspace, so the normal thread lookup covers cloud topics and
// this check would wrongly skip local ones.
func defaultCloudOwned(home string) relayCloudOwnedFunc {
	return func(label string) (bool, error) {
		cloudHome, ok, err := workspaceconfig.CloudHome(home)
		if err != nil {
			return false, err
		}
		if !ok || sameWorkspace(home, cloudHome) {
			return false, nil
		}
		client := bd.NewClientWithEnv(cloudHome, hostedTLSEnv)
		return cloudLabelExists(client.RunContext, label, cloudOwnedTimeout)
	}
}

// cloudLabelExists runs `bd list --all --label=<label> --json --limit 1`
// through run under a deadline of timeout and reports whether at least one
// issue came back. A deadline expiry returns an error.
func cloudLabelExists(run cloudRunFunc, label string, timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := run(ctx, "list", "--all", "--label="+label, "--json", "--limit", "1")
	if err != nil {
		return false, err
	}
	var issues []bd.Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return false, fmt.Errorf("hosted label query: unmarshal: %w (raw: %.200s)", err, out)
	}
	return len(issues) > 0, nil
}
