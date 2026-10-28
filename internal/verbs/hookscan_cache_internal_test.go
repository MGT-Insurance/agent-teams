package verbs

import (
	"os"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// tie-session drops the hook-scan cache entry for its session id, so a re-tie
// takes effect on the next prompt instead of after the TTL.
func TestTieSession_InvalidatesHookScanCache(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			if args[0] == "show" {
				return issueJSON(bd.Issue{ID: "at-target", Description: "problem: x\n"}), nil
			}
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			if issues, ok := dst.(*[]bd.Issue); ok {
				*issues = []bd.Issue{{ID: "at-target", Status: "open", Description: "problem: x\n"}}
			}
			return nil
		},
	}
	home := t.TempDir()
	ctx, _, _ := makeCtx(fbd, home)

	writeHookScanCache(ctx, "sess-1", hookScanCacheEntry{ID: "at-old", CheckedAt: time.Now(), Trusted: true})
	writeHookScanCache(ctx, "sess-other", hookScanCacheEntry{ID: "at-x", CheckedAt: time.Now(), Trusted: true})

	cmd := &tieSessionKong{InitiativeID: "at-target", SessionID: "sess-1"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("tie-session: %v", err)
	}
	if _, err := os.Stat(hookScanCachePath(ctx, "sess-1")); !os.IsNotExist(err) {
		t.Errorf("cache for the tied session survived: %v", err)
	}
	if _, ok := readHookScanCache(ctx, "sess-other"); !ok {
		t.Error("tie-session deleted another session's cache")
	}
}
