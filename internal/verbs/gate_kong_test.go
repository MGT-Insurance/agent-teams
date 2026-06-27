// gate_kong_test.go: core-path kong-parse tests for gateKong.
// Verifies the xor:"gateform" boundary: --file and --decision remain
// mutually exclusive, while the structured companions (--recommendation,
// --alternative, --context-file) combine freely alongside --decision.
package verbs

import (
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// buildGateParser builds a minimal cli.Parser with only the gate verb wired,
// and returns both the parser and the underlying gateKong struct (populated
// in-place by Parse).
func buildGateParser(t *testing.T) (*cli.Parser, *gateKong) {
	t.Helper()
	cmd := &gateKong{}
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	p.AddVerb("gate", "Add a gate.", cmd)
	return p, cmd
}

// TestGateKong_StructuredFlagsCombineFreely proves that all four structured
// flags parse together without an XOR conflict, and that the parsed fields
// hold the expected values.
func TestGateKong_StructuredFlagsCombineFreely(t *testing.T) {
	// --context-file must point to a real file that Validate can stat and read.
	f, err := os.CreateTemp(t.TempDir(), "ctx-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString("some context"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	p, cmd := buildGateParser(t)
	_, parseErr := p.Parse([]string{
		"gate", "at-x1",
		"--decision", "Should we ship?",
		"--recommendation", "Yes",
		"--alternative", "No",
		"--context-file", f.Name(),
	})
	if parseErr != nil {
		t.Fatalf("structured flags should not conflict; got error: %v", parseErr)
	}
	if cmd.Decision != "Should we ship?" {
		t.Errorf("Decision = %q, want %q", cmd.Decision, "Should we ship?")
	}
	if cmd.Recommendation != "Yes" {
		t.Errorf("Recommendation = %q, want %q", cmd.Recommendation, "Yes")
	}
	if cmd.Alternative != "No" {
		t.Errorf("Alternative = %q, want %q", cmd.Alternative, "No")
	}
	if cmd.ContextFile != f.Name() {
		t.Errorf("ContextFile = %q, want %q", cmd.ContextFile, f.Name())
	}
}

// TestGateKong_FileAndDecisionConflict proves that --file and --decision
// remain mutually exclusive (xor:"gateform" boundary preserved).
func TestGateKong_FileAndDecisionConflict(t *testing.T) {
	p, _ := buildGateParser(t)
	_, err := p.Parse([]string{
		"gate", "at-x1",
		"--file", "/dev/null",
		"--decision", "D",
	})
	if err == nil {
		t.Fatal("expected XOR conflict error for --file + --decision; got nil")
	}
	if !strings.Contains(err.Error(), "can't be used together") {
		t.Errorf("error %q does not mention XOR conflict", err.Error())
	}
}
