// Package verbs — prmerge.go implements agent-teams-p9dm.24: the ONE piece
// of GitHub-derived signal that survived the at-jno7 re-decomposition — is a
// PR MERGED or CLOSED — plus the TTL cache that keeps execution-status
// (agent-teams-p9dm.17, status.go) from shelling out to `gh` on every
// invocation.
//
// Every name/constant/schema/TTL this file depends on is FROZEN by the
// contract in external_review.go (agent-teams-p9dm.22 §6, §8): ghProbeJSONFields,
// prStateFileName/prStatePath/prStateKey, prStateFile/prStateEntry/
// prStateSchemaVersion, prProbeSuccessTTL/prProbeFailureTTL/prProbeTimeout,
// and the DEGRADE CONTRACT this file implements. Nothing here redefines any
// of those.
//
// 🚫 Per contract §0: this probe requests --json state and NOTHING else.
// reviewDecision, reviewRequests, and latestReviews must never be fetched —
// TestDefaultPRMerge_ArgvExact is the regression guard on that prohibition.
package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// ── §6: the probe seam ───────────────────────────────────────────────────

// prMergeFunc is the function type for probing one PR's merge state.
// Injected so tests never shell out to real gh, mirroring labelAddFunc
// (notify.go:22-24) / dirExistsFunc (hung_scan.go).
type prMergeFunc func(ownerRepo string, prNumber int) (string, error)

// defaultPRMerge runs, verbatim from contract §6:
//
//	gh pr view <n> --repo <owner/repo> --json state
//
// bounded by prProbeTimeout, and parses the single "state" field. Failure
// taxonomy is FLAT per the contract's DEGRADE CONTRACT (external_review.go
// §8): a non-zero exit, an HTTP 404, and unparseable JSON on stdout all
// return a plain error — no branching, no retry, no partial parsing. The
// caller (this file's cache, and eventually status.go) never sees which
// failure mode occurred, only that the probe failed.
func defaultPRMerge(ownerRepo string, prNumber int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), prProbeTimeout)
	defer cancel()

	// argv is EXACTLY the contract's command — do not add fields here. See
	// the package doc comment: this line is the enforcement mechanism for
	// contract §0's prohibition, guarded by TestDefaultPRMerge_ArgvExact.
	out, err := exec.CommandContext(ctx, "gh", "pr", "view", strconv.Itoa(prNumber), "--repo", ownerRepo, "--json", ghProbeJSONFields).Output()
	if err != nil {
		return "", fmt.Errorf("gh pr view: %w", err)
	}

	var parsed struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("parse gh pr view output: %w", err)
	}
	return parsed.State, nil
}

// prMergePreflight checks, once per invocation, whether probing is possible
// at all: gh must be on PATH and `gh auth status` must exit 0. A non-nil
// result means the caller skips ALL probes for the run (contract §8's
// PREFLIGHT degrade path) — every initiative falls back to pr_probe=
// prProbeUnreachable and the declared-label answer, which needs no probe.
func prMergePreflight() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh not found on PATH: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), prProbeTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "gh", "auth", "status").Run(); err != nil {
		return fmt.Errorf("gh auth status: %w", err)
	}
	return nil
}

// ── §8: the TTL cache ────────────────────────────────────────────────────

// prStateCache is the in-memory working set behind prStateFile
// (external_review.go): loadPRStateCache/storePRStateCache (de)serialize it
// against <home>/pr-state.json; lookup/putOK/putFailure are the per-probe
// read/write operations a caller runs between those two calls.
type prStateCache struct {
	entries map[string]prStateEntry
}

// loadPRStateCache reads the cache file at <home>/pr-state.json (prStatePath's
// path, computed here from a plain home string rather than *cli.Context so
// callers — and tests — can pass ctx.Home directly without constructing a
// full context, mirroring hung_scan.go's ctx.Home-derived-path convention).
// A missing or corrupt file loads as EMPTY and is not an error, mirroring
// loadHungState (hung_scan.go:183-196).
func loadPRStateCache(home string) prStateCache {
	empty := prStateCache{entries: map[string]prStateEntry{}}

	data, err := os.ReadFile(filepath.Join(home, prStateFileName))
	if err != nil {
		return empty
	}
	var f prStateFile
	if err := json.Unmarshal(data, &f); err != nil {
		return empty
	}
	if f.Entries == nil {
		return empty
	}
	return prStateCache{entries: f.Entries}
}

// storePRStateCache writes c to <home>/pr-state.json. The write is atomic —
// a temp file in the same directory, then os.Rename over the target —
// mirroring saveHungState (hung_scan.go:210-244) so a concurrent reader
// (execution-status can run concurrently per contract §8's CONCURRENCY
// note) never observes a torn write; at worst a lost update costs one
// redundant probe next time.
func storePRStateCache(home string, c prStateCache) error {
	path := filepath.Join(home, prStateFileName)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}

	entries := c.entries
	if entries == nil {
		entries = map[string]prStateEntry{}
	}
	data, err := json.Marshal(prStateFile{SchemaVersion: prStateSchemaVersion, Entries: entries})
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "pr-state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp state: %w", err)
	}
	// os.CreateTemp makes the file 0o600; match the cache file's intended
	// 0o644 so the atomic rewrite doesn't silently tighten permissions.
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename state into place: %w", err)
	}
	return nil
}

// lookup returns the cached entry for key and whether it is still fresh as
// of now: within prProbeSuccessTTL for an OK entry, prProbeFailureTTL for a
// failure entry — both flat, no backoff ladder (contract §8). fresh==false
// means the caller must probe: no entry exists, its ProbedAt failed to
// parse, or its TTL has elapsed.
func (c *prStateCache) lookup(key string, now time.Time) (prStateEntry, bool) {
	entry, ok := c.entries[key]
	if !ok {
		return prStateEntry{}, false
	}
	probedAt, err := time.Parse(time.RFC3339, entry.ProbedAt)
	if err != nil {
		return entry, false
	}
	ttl := prProbeFailureTTL
	if entry.OK {
		ttl = prProbeSuccessTTL
	}
	return entry, now.Sub(probedAt) < ttl
}

// putOK records a successful probe result for key.
func (c *prStateCache) putOK(key, state string, now time.Time) {
	if c.entries == nil {
		c.entries = map[string]prStateEntry{}
	}
	c.entries[key] = prStateEntry{
		ProbedAt: now.UTC().Format(time.RFC3339),
		OK:       true,
		State:    state,
	}
}

// putFailure records a failed probe result for key. isNew is false when key
// already held a live (fresh, per prProbeFailureTTL) failure entry — this
// is the ONLY thing that authorizes the caller's single stderr line
// (contract §8). Because the normal caller only probes (and thus only calls
// putFailure) after lookup already reported fresh==false, this branch is
// reachable in practice only once a prior failure entry has expired — which
// is exactly what keeps a permanently dead PR to one stderr line per
// prProbeFailureTTL instead of one line per invocation under the hung tick.
//
// When a live failure entry already exists, it is left untouched rather
// than having its ProbedAt refreshed — mirroring hungAnchor's
// carry-forward-unless-a-new-episode-starts pattern (hung_scan.go) — so the
// TTL window stays anchored to the ORIGINAL failure even if a caller ever
// calls putFailure without going through lookup's gate first.
func (c *prStateCache) putFailure(key string, probeErr error, now time.Time) bool {
	if existing, ok := c.entries[key]; ok && !existing.OK {
		if probedAt, err := time.Parse(time.RFC3339, existing.ProbedAt); err == nil {
			if now.Sub(probedAt) < prProbeFailureTTL {
				return false
			}
		}
	}

	if c.entries == nil {
		c.entries = map[string]prStateEntry{}
	}
	c.entries[key] = prStateEntry{
		ProbedAt: now.UTC().Format(time.RFC3339),
		OK:       false,
		Error:    probeErr.Error(),
	}
	return true
}
