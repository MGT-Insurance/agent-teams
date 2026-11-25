package verbs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsPOStateKeyTierWordsStayLearnings(t *testing.T) {
	for k, want := range map[string]bool{
		"po:quoting:view":             true,
		"po:quoting:claim:at-1":       true,
		"po:quoting:said:at-1":        true,
		"po:quoting:reading:f:s:2026": true,
		"po:hot:view":                 false,
		"po:fresh:view":               false,
		"po:cold:claim:x":             false,
		"po:hot:some-slug":            false,
		"po:view":                     false,
		"implementer:quoting:view":    false,
		"po-quoting:hot:view":         false,
	} {
		if got := isPOStateKey(k); got != want {
			t.Errorf("isPOStateKey(%q)=%v want %v", k, got, want)
		}
	}
	raw := map[string]string{"po:hot:view": "learning", "po:quoting:view": "state"}
	dropPOStateKeys(raw)
	if _, ok := raw["po:hot:view"]; !ok || len(raw) != 1 {
		t.Fatalf("guard dropped wrong keys: %v", raw)
	}
}

func TestSeedAndTopicsRouteThroughPOGuard(t *testing.T) {
	for _, k := range []string{"po:quoting:view", "po:quoting:claim:at-1", "po:steward:view", "po:steward:topics:h"} {
		if ns, ok := seedNamespace(k); ok {
			t.Errorf("seedNamespace(%q) = %q, want out of scope", k, ns)
		}
	}
	rec, _ := StewardTopicsRecord{Briefing: "b1", Reviews: "r1"}.Marshal()
	fbd := &fakeBD{runJSONFn: func(dst any, args ...string) error {
		*dst.(*map[string]any) = map[string]any{"steward:topics:h": rec}
		return nil
	}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	if !isKnownStewardTopic(ctx, "b1") {
		t.Fatal("real steward topics record must still resolve")
	}
	// A PO state key can never be read as a steward topics record.
	poRec, _ := json.Marshal(StewardTopicsRecord{Briefing: "pb"})
	fbd2 := &fakeBD{runJSONFn: func(dst any, args ...string) error {
		*dst.(*map[string]any) = map[string]any{"po:steward:view": string(poRec), "po:steward:claim:" + strings.TrimPrefix(stewardTopicsKeyPrefix, "steward:topics:") + "x": string(poRec)}
		return nil
	}}
	ctx2, _, _ := makeCtx(fbd2, t.TempDir())
	if isKnownStewardTopic(ctx2, "pb") {
		t.Fatal("PO state key read as a steward topic")
	}
}
