// Package eval implements the agent-teams eval harness: it dispatches DRI
// runs under different agent-teams configurations (model, advisor on/off),
// extracts cost/wall-clock/tool-call metrics, judges correctness against a
// task's acceptance criteria, and pushes descriptive scores to Langfuse for
// cross-configuration comparison. It is NOT a pass/fail gate.
//
// WARNING: Run and Judge (behind the `eval run` and `eval collect` CLI
// commands) dispatch a real agent-teams session and call a real LLM judge —
// both spend actual API dollars and real wall-clock time (observed: ~$9 and
// 13 minutes for one bugfix run; runs can take hours). Never call them from
// a test, CI job, or loop. This package's own test suite fakes every such
// seam (runGitClone, runDispatch, runExtractMetrics, runJudge, runPush) and
// costs nothing to run. See eval/README.md for the full cost model.
//
// This package is the frozen contract from bead agent-teams-grft.1: file
// layout, data shapes, and the four exposed entry points (Run,
// ExtractMetrics, Judge, Push) are specified there and must not be changed
// without editing that bead and notifying the DRI.
package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// ConfigFingerprint identifies one agent-teams configuration under test.
type ConfigFingerprint struct {
	Name     string `json:"name"`     // human label, e.g. "opus-noadvisor"
	DRIModel string `json:"driModel"` // --model value: "opus" | "sonnet" | "fable"
	Advisor  string `json:"advisor"`  // --advisor value; "" = off
	// Reserved for R4 (plugin-variant ring); zero in v1:
	PerRoleModels     map[string]string `json:"perRoleModels,omitempty"`
	PromptVariantHash string            `json:"promptVariantHash,omitempty"`
	PluginRef         string            `json:"pluginRef,omitempty"`
}

// Hash() returns a short (12-hex) sha256 over canonical JSON of the struct.
// Used as the stable tag on the Langfuse experiment run.
//
// encoding/json is already canonical for this type: struct fields marshal in
// declaration order and map keys marshal in sorted order, so no separate
// canonicalization step is needed.
func (c ConfigFingerprint) Hash() string {
	data, err := json.Marshal(c)
	if err != nil {
		// ConfigFingerprint holds only strings and map[string]string;
		// Marshal cannot fail for a well-formed value.
		panic(fmt.Sprintf("eval: ConfigFingerprint.Hash: marshal: %v", err))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:6])
}
