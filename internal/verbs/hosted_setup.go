package verbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/plugins/agent-teams/templates"
)

// eventsJournalKey is the bd config key that makes bd write the events journal
// the Dispatcher's autostart reads.
const eventsJournalKey = "events-journal"

// ensureHostedHomeSetup applies the full local-disk setup a hosted home needs.
// A hosted home is an ateam workspace whose .beads/ is bound to a hosted Dolt
// database. bd keeps part of its setup on local disk, so it does not travel
// with the database: events-journal and the prime caps live in
// <home>/.beads/config.yaml, and PRIME.md is a file in <home>/.beads/. The
// sentinel memory lives in the database itself.
//
// This is the single list of that setup; ctx.Home must be the hosted home and
// ctx.BD a client bound to it. Each local item is checked by reading the file
// directly, so a fully set up home makes no bd call. A missing item is
// installed through the same helpers `ateam steward init` uses. The sentinel
// is shared state, so it is checked only when checkSentinel is true or some
// local item was missing.
//
// The laptop's own workspace is not a hosted home; callers must not pass it.
func ensureHostedHomeSetup(ctx *cli.Context, checkSentinel bool) error {
	cfg := readBeadsConfigKeys(filepath.Join(ctx.Home, ".beads", "config.yaml"))
	missing := false

	if cfg[eventsJournalKey] != "true" {
		missing = true
		if _, err := ctx.BD.Run("config", "set", eventsJournalKey, "true"); err != nil {
			return fmt.Errorf("set %s: %w", eventsJournalKey, err)
		}
	}

	if _, err := os.Stat(globalPrimeMDPath(ctx)); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", globalPrimeMDPath(ctx), err)
		}
		missing = true
		if err := installGlobalPrimeMD(ctx, templates.GlobalPrimeMD); err != nil {
			return fmt.Errorf("install PRIME.md: %w", err)
		}
	}

	if cfg[primeMaxMemoriesKey] != primeMemoryCapValue || cfg[primeMaxMemoryCharsKey] != primeMemoryCapValue {
		missing = true
		if err := installPrimeMemoryCaps(ctx); err != nil {
			return fmt.Errorf("install prime memory caps: %w", err)
		}
	}

	if missing || checkSentinel {
		if err := installSentinelMemory(ctx); err != nil {
			return fmt.Errorf("install prime cap sentinel memory: %w", err)
		}
	}
	return nil
}

// ensureHostedHomeSetupSoft runs ensureHostedHomeSetup and reports a failure
// to stderr as a warning instead of returning it. Callers use it where the
// surrounding operation does not itself need the setup.
func ensureHostedHomeSetupSoft(ctx *cli.Context, checkSentinel bool) {
	if err := ensureHostedHomeSetup(ctx, checkSentinel); err != nil {
		fmt.Fprintf(ctx.Stderr, "WARNING: hosted workspace setup for %s failed (continuing): %v\n", ctx.Home, err)
	}
}

// readBeadsConfigKeys scans a bd config.yaml for scalar settings and returns
// them keyed by dotted path. It understands the two layouts bd writes: a flat
// `a.b: v` line and one level of nesting (`a:` followed by indented `b: v`).
// Quotes and trailing comments are stripped. A missing or unreadable file
// returns an empty map. Anything it cannot parse reads as unset, which makes
// the caller run an idempotent `bd config set`, so a misread costs one call
// and never skips needed setup.
func readBeadsConfigKeys(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	parent := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, val, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if i := strings.Index(val, " #"); i >= 0 {
			val = val[:i]
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		indented := line[0] == ' ' || line[0] == '\t'
		switch {
		case !indented && val == "":
			parent = key
		case !indented:
			parent = ""
			out[key] = val
		case parent != "" && val != "":
			out[parent+"."+key] = val
		}
	}
	return out
}
