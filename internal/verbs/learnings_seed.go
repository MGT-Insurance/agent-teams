package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// seedNamespaces are the key prefixes learnings-seed copies, in report order.
// Every other key (000-prime-cap-sentinel, schema_version, ...) is out of
// scope. The steward namespace copies in every tier (hot, fresh, and cold)
// except the keys under seedExcludePrefixes.
var seedNamespaces = []string{"dri", "planner", "implementer", "tester", "reviewer", "investigator", "steward", "user", "applied"}

// seedExcludePrefixes are key prefixes that stay local even when their
// namespace is in scope. steward:topics:<host> records are per-machine.
var seedExcludePrefixes = []string{stewardTopicsKeyPrefix}

// learningsSeedKong is the kong form of learnings-seed.
type learningsSeedKong struct {
	DryRun bool `name:"dry-run" help:"Print what would be copied without writing."`
}

// seedMemoryLine is one memory-only JSONL line for bd import.
type seedMemoryLine struct {
	Type  string `json:"_type"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// seedCounts holds the per-namespace tallies learnings-seed reports.
type seedCounts struct{ local, hosted, toWrite, kept, keptSlug int }

// Run copies local role and user learnings that are absent from the hosted
// learnings store into it, in one bd import. It never overwrites a hosted value.
// A local key counts as present when the hosted store holds the same role and
// slug under any tier (see seedIdentity).
func (c *learningsSeedKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam learnings-seed: no context")
	}
	if os.Getenv("DSP_BEAD") != "" {
		return cli.Usagef("learnings-seed: refusing to run inside a Dispatcher run (DSP_BEAD is set)")
	}
	if _, ok, err := workspaceconfig.CloudHome(ctx.Home); err != nil {
		return fmt.Errorf("ateam learnings-seed: %w", err)
	} else if !ok {
		return cli.Usagef("learnings-seed: cloud_home is not set in config.toml; nothing to seed")
	}
	if !learnRemote(ctx) {
		return cli.Usagef("learnings-seed: cloud_home names this workspace; nothing to seed")
	}
	dst := learnBD(ctx)

	var local, hosted map[string]any
	if err := readMemoriesJSON(ctx.BD, &local); err != nil {
		return fmt.Errorf("ateam learnings-seed: read local memories: %w", err)
	}
	if err := readMemoriesJSON(dst, &hosted); err != nil {
		return learnReadFailure(ctx, "learnings-seed", err)
	}

	hostedByIdentity := map[string]string{}
	var hostedKeys []string
	for k := range hosted {
		hostedKeys = append(hostedKeys, k)
	}
	sort.Strings(hostedKeys)
	for _, k := range hostedKeys {
		if _, ok := seedNamespace(k); !ok {
			continue
		}
		if _, isStr := hosted[k].(string); !isStr {
			continue
		}
		id := seedIdentity(k)
		if _, seen := hostedByIdentity[id]; !seen {
			hostedByIdentity[id] = k
		}
	}

	counts := map[string]*seedCounts{}
	for _, ns := range seedNamespaces {
		counts[ns] = &seedCounts{}
	}
	for k, v := range hosted {
		if ns, ok := seedNamespace(k); ok {
			if _, isStr := v.(string); isStr {
				counts[ns].hosted++
			}
		}
	}

	var keys []string
	for k := range local {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	prefix := ""
	if c.DryRun {
		prefix = "[dry-run] "
	}
	var missing []seedMemoryLine
	var keptLines []string
	for _, k := range keys {
		ns, ok := seedNamespace(k)
		if !ok {
			continue
		}
		lv, isStr := local[k].(string)
		if !isStr {
			continue
		}
		counts[ns].local++
		hv, present := hosted[k]
		switch {
		case !present:
			if hk, same := hostedByIdentity[seedIdentity(k)]; same {
				counts[ns].keptSlug++
				keptLines = append(keptLines, fmt.Sprintf("%slearnings-seed: kept hosted %s (same slug as local %s)", prefix, hk, k))
				continue
			}
			counts[ns].toWrite++
			missing = append(missing, seedMemoryLine{Type: "memory", Key: k, Value: lv})
		case hv != lv:
			counts[ns].kept++
			keptLines = append(keptLines, fmt.Sprintf("%slearnings-seed: kept hosted %s (differs)", prefix, k))
		}
	}

	var total seedCounts
	for _, ns := range seedNamespaces {
		n := counts[ns]
		fmt.Fprintf(ctx.Stdout, "%slearnings-seed %s: local %d hosted %d to-write %d kept-differs %d kept-same-slug %d\n",
			prefix, ns, n.local, n.hosted, n.toWrite, n.kept, n.keptSlug)
		total.local += n.local
		total.hosted += n.hosted
		total.toWrite += n.toWrite
		total.kept += n.kept
		total.keptSlug += n.keptSlug
	}
	for _, l := range keptLines {
		fmt.Fprintln(ctx.Stdout, l)
	}
	fmt.Fprintf(ctx.Stdout, "%slearnings-seed total: local %d hosted %d to-write %d kept-differs %d kept-same-slug %d\n",
		prefix, total.local, total.hosted, total.toWrite, total.kept, total.keptSlug)

	if c.DryRun {
		fmt.Fprintf(ctx.Stdout, "[dry-run] learnings-seed: would import %d keys\n", len(missing))
		return nil
	}
	if len(missing) == 0 {
		fmt.Fprintln(ctx.Stdout, "learnings-seed: imported 0 keys in 0.0 s")
		return nil
	}

	path, err := writeSeedJSONL(missing)
	if err != nil {
		return fmt.Errorf("ateam learnings-seed: %w", err)
	}
	defer os.Remove(path)
	start := time.Now()
	if _, err := dst.Run("import", path); err != nil {
		return learnWriteFailure(ctx, "learnings-seed", err)
	}
	fmt.Fprintf(ctx.Stdout, "learnings-seed: imported %d keys in %.1f s\n", len(missing), time.Since(start).Seconds())
	return nil
}

// seedNamespace returns the in-scope namespace of key k.
func seedNamespace(k string) (string, bool) {
	if isPOStateKey(k) {
		return "", false
	}
	for _, p := range seedExcludePrefixes {
		if strings.HasPrefix(k, p) {
			return "", false
		}
	}
	idx := strings.Index(k, ":")
	if idx <= 0 {
		return "", false
	}
	ns := k[:idx]
	for _, s := range seedNamespaces {
		if s == ns {
			return ns, true
		}
	}
	return "", false
}

// seedIdentity returns the role-and-slug identity of key k, ignoring the tier
// segment. Role learnings are stored as <role>:hot:<slug>, <role>:fresh:<slug>,
// or untiered <role>:<slug>, and the hosted condense pass rewrites tiered keys
// to untiered ones, so the same learning appears under different full keys.
// applied:<role>:<slug> records carry no tier segment and keep their full key.
func seedIdentity(k string) string {
	ns, rest, ok := strings.Cut(k, ":")
	if !ok || ns == "applied" {
		return k
	}
	if r, found := strings.CutPrefix(rest, "hot:"); found {
		rest = r
	} else if r, found := strings.CutPrefix(rest, "fresh:"); found {
		rest = r
	}
	return ns + ":" + rest
}

// writeSeedJSONL writes one memory-only JSONL line per entry to a temp file and
// returns its path.
func writeSeedJSONL(lines []seedMemoryLine) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			return "", err
		}
	}
	f, err := os.CreateTemp("", "learnings-seed-*.jsonl")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(buf.Bytes()); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// RegisterLearningsSeed wires learnings-seed into the parser.
func RegisterLearningsSeed(p *cli.Parser) {
	p.AddVerb("learnings-seed", "Copy local role, user, and steward learnings (all tiers; per-machine steward:topics:<host> records stay local) into the hosted learnings store (cloud_home); never overwrites, and skips a key whose role and slug already exist hosted under any tier.", &learningsSeedKong{})
}
