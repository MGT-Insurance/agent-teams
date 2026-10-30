// hookscan_cache.go is the per-session result cache behind `ateam hook-scan`
// (agent-teams-c4i9.20.34.7). The hook needs the initiative id on every
// prompt, but needs bd only to count unread mail. A trusted cached result with
// no unread mail and no doorbell lets a quiet prompt skip bd entirely; the
// full scan still runs on a TTL, on every doorbell, and while mail is unread.
//
// The cache lives at <home>/cache/hookscan/<key>.json. The key is the session
// id (path form) or the --id value (steward form). Every cache failure is
// silent and means "take the full path": the cache is an optimization, never
// a source of errors for a hook.
package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// hookScanCacheTTL bounds how stale a trusted cache entry may be before the
// next prompt rescans. Mail that arrives without a doorbell surfaces at the
// first prompt after this window.
const hookScanCacheTTL = 10 * time.Minute

// hookScanCacheEntry is the on-disk cache record. Trusted is false when the
// scan that wrote it ran with a doorbell present: inbox-drain.sh deletes the
// doorbell after hook-scan returns, so mail that lands between the list and
// that delete loses its doorbell, and the next prompt must rescan.
type hookScanCacheEntry struct {
	ID        string    `json:"id"`
	Repo      string    `json:"repo"`
	Unread    int       `json:"unread"`
	CheckedAt time.Time `json:"checked_at"`
	Trusted   bool      `json:"trusted"`
}

// fresh reports whether the entry is younger than the TTL. An entry stamped in
// the future (clock moved backwards) is not fresh.
func (e hookScanCacheEntry) fresh(now time.Time) bool {
	age := now.Sub(e.CheckedAt)
	return age >= 0 && age < hookScanCacheTTL
}

var hookScanCacheKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// hookScanCachePath returns the cache file for key, or "" when the key is
// empty or not a safe file name (no cache is used then).
func hookScanCachePath(ctx *cli.Context, key string) string {
	if ctx.Home == "" || !hookScanCacheKeyRe.MatchString(key) {
		return ""
	}
	return filepath.Join(ctx.Home, "cache", "hookscan", key+".json")
}

// readHookScanCache returns the entry for key. A missing, unreadable, or
// unparsable file returns ok=false.
func readHookScanCache(ctx *cli.Context, key string) (hookScanCacheEntry, bool) {
	var e hookScanCacheEntry
	path := hookScanCachePath(ctx, key)
	if path == "" {
		return e, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return e, false
	}
	if err := json.Unmarshal(data, &e); err != nil || e.ID == "" {
		return hookScanCacheEntry{}, false
	}
	return e, true
}

// writeHookScanCache stores e for key atomically (temp file + rename). Errors
// are ignored.
func writeHookScanCache(ctx *cli.Context, key string, e hookScanCacheEntry) {
	path := hookScanCachePath(ctx, key)
	if path == "" {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// deleteHookScanCache removes the entry for key, if any.
func deleteHookScanCache(ctx *cli.Context, key string) {
	if path := hookScanCachePath(ctx, key); path != "" {
		_ = os.Remove(path)
	}
}

// invalidateHookScanCacheUnless removes the entry for key unless an entry
// exists and names keepID. An empty keepID always removes it.
func invalidateHookScanCacheUnless(ctx *cli.Context, key, keepID string) {
	if keepID != "" {
		if e, ok := readHookScanCache(ctx, key); ok && e.ID == keepID {
			return
		}
	}
	deleteHookScanCache(ctx, key)
}

// hookScanDoorbellPresent reports whether initiative id has an unconsumed
// doorbell file.
func hookScanDoorbellPresent(ctx *cli.Context, id string) bool {
	_, err := os.Stat(filepath.Join(ctx.Home, "mailbox", id+".wake"))
	return err == nil
}
