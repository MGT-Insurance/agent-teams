package verbs

import (
	"fmt"
	"os"
	"testing"
)

// TestPublishStewardTopics_WritesRecord verifies publishStewardTopics reads
// this machine's local briefing/direct thread-ref files and upserts them as
// a StewardTopicsRecord under StewardTopicsKey(os.Hostname()) via the raw
// "remember" storage path (not learnKey).
func TestPublishStewardTopics_WritesRecord(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			cp := make([]string, len(args))
			copy(cp, args)
			calls = append(calls, cp)
			return "", nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if err := writeThreadRefFile(StewardBriefingThreadPath(ctx), "briefing-ref-1"); err != nil {
		t.Fatalf("seed briefing thread ref: %v", err)
	}
	if err := writeThreadRefFile(StewardDirectThreadPath(ctx), "direct-ref-1"); err != nil {
		t.Fatalf("seed direct thread ref: %v", err)
	}

	if err := publishStewardTopics(ctx); err != nil {
		t.Fatalf("publishStewardTopics: %v", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname: %v", err)
	}
	wantKey := "--key=" + StewardTopicsKey(hostname)

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d: %v", len(calls), calls)
	}
	assertCall(t, calls[0], "remember", wantKey)

	got, err := ParseStewardTopicsRecord(calls[0][2])
	if err != nil {
		t.Fatalf("ParseStewardTopicsRecord: %v", err)
	}
	want := StewardTopicsRecord{Briefing: "briefing-ref-1", Direct: "direct-ref-1"}
	if got != want {
		t.Errorf("published record = %+v, want %+v", got, want)
	}
}

// TestPublishStewardTopics_NilContext verifies nil context returns an error.
func TestPublishStewardTopics_NilContext(t *testing.T) {
	if err := publishStewardTopics(nil); err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// TestPublishStewardTopics_BDErrorPropagates verifies bd "remember" failures
// are returned as errors.
func TestPublishStewardTopics_BDErrorPropagates(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			return "", fmt.Errorf("bd remember: simulated failure")
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	if err := publishStewardTopics(ctx); err == nil {
		t.Fatal("expected error from bd failure; got nil")
	}
}

// TestIsKnownStewardTopic_RoundTrip is the core-path loop-closing test:
// publish this machine's own refs, seed a peer machine's record directly in
// the fake store, and verify isKnownStewardTopic distinguishes all three
// cases required by the bead's acceptance criteria: own refs -> false,
// seeded other-steward ref -> true, unknown ref -> false.
func TestIsKnownStewardTopic_RoundTrip(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname: %v", err)
	}

	ownRecordJSON, err := StewardTopicsRecord{Briefing: "own-briefing", Direct: "own-direct"}.Marshal()
	if err != nil {
		t.Fatalf("Marshal own record: %v", err)
	}
	peerRecordJSON, err := StewardTopicsRecord{Briefing: "peer-briefing", Direct: "peer-direct"}.Marshal()
	if err != nil {
		t.Fatalf("Marshal peer record: %v", err)
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{
				StewardTopicsKey(hostname):     ownRecordJSON,
				StewardTopicsKey("other-host"): peerRecordJSON,
				"planner:some-cold":            "unrelated memory, must be ignored",
			}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	// This machine's own local refs, as they'd be persisted by notify.go.
	if err := writeThreadRefFile(StewardBriefingThreadPath(ctx), "own-briefing"); err != nil {
		t.Fatalf("seed own briefing thread ref: %v", err)
	}
	if err := writeThreadRefFile(StewardDirectThreadPath(ctx), "own-direct"); err != nil {
		t.Fatalf("seed own direct thread ref: %v", err)
	}

	cases := []struct {
		name      string
		threadRef string
		want      bool
	}{
		{"own briefing ref", "own-briefing", false},
		{"own direct ref", "own-direct", false},
		{"peer briefing ref", "peer-briefing", true},
		{"peer direct ref", "peer-direct", true},
		{"unknown ref", "some-other-thread-ref", false},
	}
	for _, tc := range cases {
		if got := isKnownStewardTopic(ctx, tc.threadRef); got != tc.want {
			t.Errorf("isKnownStewardTopic(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestIsKnownStewardTopic_NilContext verifies a nil context fails closed.
func TestIsKnownStewardTopic_NilContext(t *testing.T) {
	if isKnownStewardTopic(nil, "some-ref") {
		t.Error("isKnownStewardTopic(nil, ...) = true, want false")
	}
}

// TestIsKnownStewardTopic_EmptyThreadRef verifies an empty threadRef fails
// closed without querying the store.
func TestIsKnownStewardTopic_EmptyThreadRef(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			t.Fatal("isKnownStewardTopic should not query the store for an empty threadRef")
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	if isKnownStewardTopic(ctx, "") {
		t.Error("isKnownStewardTopic(ctx, \"\") = true, want false")
	}
}

// TestIsKnownStewardTopic_BDErrorFailsClosed verifies a memory-store read
// error degrades to false rather than panicking or propagating.
func TestIsKnownStewardTopic_BDErrorFailsClosed(t *testing.T) {
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("bd memories: simulated failure")
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	if isKnownStewardTopic(ctx, "some-ref") {
		t.Error("isKnownStewardTopic on bd error = true, want false")
	}
}
