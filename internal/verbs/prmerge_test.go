package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ── test helpers ─────────────────────────────────────────────────────────

// withFakeGH puts a fake `gh` executable (script) at the front of PATH for
// the duration of the calling test, restoring PATH on cleanup. Mirrors the
// fake-`ateam`-on-PATH pattern in relay_supervise_test.go
// (TestDefaultRelaySpawn_PinsAgentTeamsHomeEnv).
func withFakeGH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
}

// ── defaultPRMerge ───────────────────────────────────────────────────────

// TestDefaultPRMerge_ArgvExact is the regression guard on contract §0's
// prohibition (external_review.go): the probe must request --json state and
// NOTHING else. If a future change widens this to also ask for
// reviewDecision, reviewRequests, or latestReviews, this test fails because
// the captured argv no longer matches exactly.
func TestDefaultPRMerge_ArgvExact(t *testing.T) {
	captureFile := filepath.Join(t.TempDir(), "captured-argv")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %q
echo '{"state":"MERGED"}'
`, captureFile)
	withFakeGH(t, script)

	state, err := defaultPRMerge("mgt-insurance/midgard", 4501)
	if err != nil {
		t.Fatalf("defaultPRMerge: %v", err)
	}
	if state != "MERGED" {
		t.Errorf("state = %q, want MERGED", state)
	}

	data, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("read captured argv: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	want := []string{"pr", "view", "4501", "--repo", "mgt-insurance/midgard", "--json", "state"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}

func TestDefaultPRMerge_ParsesStates(t *testing.T) {
	for _, state := range []string{prStateMerged, prStateClosed, prStateOpen} {
		t.Run(state, func(t *testing.T) {
			withFakeGH(t, fmt.Sprintf("#!/bin/sh\necho '{\"state\":\"%s\"}'\n", state))

			got, err := defaultPRMerge("owner/repo", 1)
			if err != nil {
				t.Fatalf("defaultPRMerge: %v", err)
			}
			if got != state {
				t.Errorf("state = %q, want %q", got, state)
			}
		})
	}
}

func TestDefaultPRMerge_MalformedJSON_ReturnsError(t *testing.T) {
	withFakeGH(t, "#!/bin/sh\necho 'not-json'\n")

	state, err := defaultPRMerge("owner/repo", 1)
	if err == nil {
		t.Fatal("defaultPRMerge: want error for malformed JSON, got nil")
	}
	if state != "" {
		t.Errorf("state = %q, want empty string on error", state)
	}
}

func TestDefaultPRMerge_NonZeroExit_ReturnsError(t *testing.T) {
	withFakeGH(t, "#!/bin/sh\nexit 7\n")

	if _, err := defaultPRMerge("owner/repo", 1); err == nil {
		t.Fatal("defaultPRMerge: want error for non-zero gh exit, got nil")
	}
}

// ── prMergePreflight ─────────────────────────────────────────────────────

func TestPRMergePreflight_GHMissing_ReturnsError(t *testing.T) {
	// A deliberately empty PATH (no fallthrough to the old PATH, which may
	// have a real gh on this machine) so LookPath genuinely fails.
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	os.Setenv("PATH", t.TempDir())

	if err := prMergePreflight(); err == nil {
		t.Fatal("prMergePreflight: want error when gh is not on PATH, got nil")
	}
}

func TestPRMergePreflight_AuthStatusFails_ReturnsError(t *testing.T) {
	withFakeGH(t, "#!/bin/sh\nexit 1\n")

	if err := prMergePreflight(); err == nil {
		t.Fatal("prMergePreflight: want error when gh auth status fails, got nil")
	}
}

func TestPRMergePreflight_Success_ReturnsNil(t *testing.T) {
	withFakeGH(t, "#!/bin/sh\nexit 0\n")

	if err := prMergePreflight(); err != nil {
		t.Errorf("prMergePreflight: want nil, got %v", err)
	}
}

// ── prStateCache: lookup ─────────────────────────────────────────────────

func TestPRStateCache_Lookup_Missing(t *testing.T) {
	c := prStateCache{entries: map[string]prStateEntry{}}
	if _, fresh := c.lookup("owner/repo#1", time.Now()); fresh {
		t.Error("lookup: want fresh=false for missing key")
	}
}

func TestPRStateCache_Lookup_FreshOK_SkipsProbe(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := prStateCache{entries: map[string]prStateEntry{
		"owner/repo#1": {ProbedAt: now.Add(-1 * time.Minute).Format(time.RFC3339), OK: true, State: prStateMerged},
	}}

	entry, fresh := c.lookup("owner/repo#1", now)
	if !fresh {
		t.Fatal("lookup: want fresh=true within prProbeSuccessTTL")
	}
	if entry.State != prStateMerged {
		t.Errorf("entry.State = %q, want %q", entry.State, prStateMerged)
	}
}

func TestPRStateCache_Lookup_ExpiredOK_ReProbes(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := prStateCache{entries: map[string]prStateEntry{
		"owner/repo#1": {ProbedAt: now.Add(-(prProbeSuccessTTL + time.Second)).Format(time.RFC3339), OK: true, State: prStateMerged},
	}}

	if _, fresh := c.lookup("owner/repo#1", now); fresh {
		t.Error("lookup: want fresh=false once prProbeSuccessTTL has elapsed")
	}
}

func TestPRStateCache_Lookup_FreshFailure_SkipsProbe(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := prStateCache{entries: map[string]prStateEntry{
		"owner/repo#1": {ProbedAt: now.Add(-30 * time.Minute).Format(time.RFC3339), OK: false, Error: "gh: HTTP 404"},
	}}

	if _, fresh := c.lookup("owner/repo#1", now); !fresh {
		t.Error("lookup: want fresh=true within prProbeFailureTTL")
	}
}

func TestPRStateCache_Lookup_ExpiredFailure_ReProbes(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := prStateCache{entries: map[string]prStateEntry{
		"owner/repo#1": {ProbedAt: now.Add(-(prProbeFailureTTL + time.Second)).Format(time.RFC3339), OK: false, Error: "gh: HTTP 404"},
	}}

	if _, fresh := c.lookup("owner/repo#1", now); fresh {
		t.Error("lookup: want fresh=false once prProbeFailureTTL has elapsed")
	}
}

// ── prStateCache: putOK / putFailure ────────────────────────────────────

func TestPRStateCache_PutOK_RecordsFreshEntry(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := prStateCache{entries: map[string]prStateEntry{}}
	c.putOK("owner/repo#1", prStateMerged, now)

	entry, fresh := c.lookup("owner/repo#1", now)
	if !fresh || !entry.OK || entry.State != prStateMerged {
		t.Errorf("after putOK: entry=%+v fresh=%v, want fresh OK entry with state %q", entry, fresh, prStateMerged)
	}
}

// TestPRStateCache_PutFailure_IsNewThenNotNew is the direct test of the
// contract §8 invariant: PutFailure's isNew return is the ONLY thing that
// authorizes the caller's stderr line, and it must go true -> false -> true
// as the failure entry is written, re-written while still live, then
// re-written after its own TTL has elapsed.
func TestPRStateCache_PutFailure_IsNewThenNotNew(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := prStateCache{entries: map[string]prStateEntry{}}

	if isNew := c.putFailure("owner/repo#1", errors.New("gh: HTTP 404"), now); !isNew {
		t.Error("first putFailure: want isNew=true")
	}

	if isNew := c.putFailure("owner/repo#1", errors.New("gh: HTTP 404"), now.Add(time.Minute)); isNew {
		t.Error("second putFailure within prProbeFailureTTL: want isNew=false")
	}

	if isNew := c.putFailure("owner/repo#1", errors.New("gh: HTTP 404"), now.Add(prProbeFailureTTL+time.Second)); !isNew {
		t.Error("putFailure after prProbeFailureTTL has elapsed: want isNew=true")
	}
}

func TestPRStateCache_PutFailure_AfterOK_IsNew(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	c := prStateCache{entries: map[string]prStateEntry{}}
	c.putOK("owner/repo#1", prStateOpen, now)

	if isNew := c.putFailure("owner/repo#1", errors.New("timeout"), now.Add(time.Second)); !isNew {
		t.Error("putFailure following a success entry: want isNew=true (first failure observed)")
	}
}

// ── loadPRStateCache / storePRStateCache ────────────────────────────────

func TestLoadPRStateCache_MissingFile_Empty(t *testing.T) {
	c := loadPRStateCache(t.TempDir())
	if len(c.entries) != 0 {
		t.Errorf("entries = %v, want empty", c.entries)
	}
}

func TestLoadPRStateCache_CorruptFile_Empty(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, prStateFileName), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt cache file: %v", err)
	}

	c := loadPRStateCache(home)
	if len(c.entries) != 0 {
		t.Errorf("entries = %v, want empty for corrupt file", c.entries)
	}
}

func TestStorePRStateCache_RoundTrip(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	c := prStateCache{entries: map[string]prStateEntry{}}
	c.putOK("mgt-insurance/midgard#4501", prStateMerged, now)
	c.putFailure("someorg/gone#12", errors.New("gh: HTTP 404"), now)

	if err := storePRStateCache(home, c); err != nil {
		t.Fatalf("storePRStateCache: %v", err)
	}

	got := loadPRStateCache(home)
	if !reflect.DeepEqual(got.entries, c.entries) {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got.entries, c.entries)
	}

	// Atomic write: only the final file must remain, no leaked temp file.
	dirEntries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	if len(dirEntries) != 1 || dirEntries[0].Name() != prStateFileName {
		var names []string
		for _, e := range dirEntries {
			names = append(names, e.Name())
		}
		t.Errorf("state dir = %v, want exactly [%s] (temp files must be renamed away, not left behind)", names, prStateFileName)
	}
}

func TestStorePRStateCache_WritesSchemaVersion(t *testing.T) {
	home := t.TempDir()
	if err := storePRStateCache(home, prStateCache{entries: map[string]prStateEntry{}}); err != nil {
		t.Fatalf("storePRStateCache: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, prStateFileName))
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	var f prStateFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal cache file: %v", err)
	}
	if f.SchemaVersion != prStateSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", f.SchemaVersion, prStateSchemaVersion)
	}
}
