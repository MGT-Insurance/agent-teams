package verbs

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// stewardStartupLimit is the most bytes `ateam steward startup` prints. The
// Dispatcher cli_run tool caps output at 64 KiB; the verb stays below that so
// its text never reaches the Dispatcher truncated.
const stewardStartupLimit = 60 * 1024

// stewardSharedLedgerRules is the ledger rules text shared with the steward
// skill. prompt-sync generates this copy from
// promptsrc/agent-teams/skills/steward/shared-ledger.md (manifest entry
// embed.steward.shared-ledger), and `prompt-sync check` fails when they differ.
//
//go:embed embedded/steward_shared_ledger.md
var stewardSharedLedgerRules string

// stewardRulesIntro leads the ledger rules section. It sits outside the shared
// text because the shared slices keep the local skill's numbering and wording.
const stewardRulesIntro = "These are the steward ledger rules shared by the local and cloud stewards; section and step numbers refer to the local skill."

// stewardStartupKong is the kong struct for `ateam steward startup`.
type stewardStartupKong struct{}

// Run prints the steward's startup text: the shared ledger rules, the ledger
// stats, and the steward learnings. It prints all of it or nothing. If the
// output is over the limit, or either store is unreachable, it prints nothing
// to stdout and returns an error, so a caller that keeps the previous whole
// text never receives a shortened one.
func (c *stewardStartupKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward startup: nil context")
	}
	stats, err := captureStewardStats(ctx)
	if err != nil {
		return err
	}
	learnings, err := captureStewardLearnings(ctx)
	if err != nil {
		return err
	}
	text, err := assembleStewardStartup(stewardRulesIntro+"\n\n"+stewardSharedLedgerRules, stats, learnings)
	if err != nil {
		return err
	}
	_, err = io.WriteString(ctx.Stdout, text)
	return err
}

// captureStewardStats returns the output of `ateam steward ledger stats`.
func captureStewardStats(ctx *cli.Context) (string, error) {
	var out bytes.Buffer
	inner := *ctx
	inner.Stdout = &out
	if err := (&stewardLedgerStatsKong{}).Run(&inner); err != nil {
		return "", fmt.Errorf("ateam steward startup: %w", err)
	}
	return out.String(), nil
}

// captureStewardLearnings returns the output of `ateam learnings steward`. A
// run that prints the "continuing WITHOUT" notice served no learnings, so it
// is an error here.
func captureStewardLearnings(ctx *cli.Context) (string, error) {
	var out bytes.Buffer
	inner := *ctx
	inner.Stdout = &out
	if err := runLearnings(&inner, "steward"); err != nil {
		return "", fmt.Errorf("ateam steward startup: %w", err)
	}
	if strings.Contains(out.String(), learnContinuingWithout) {
		return "", fmt.Errorf("ateam steward startup: learnings store unreachable (%s)", learnStoreName(ctx))
	}
	return out.String(), nil
}

// assembleStewardStartup joins the three sections, each under a one-line
// heading, and refuses output over stewardStartupLimit bytes.
func assembleStewardStartup(rules, stats, learnings string) (string, error) {
	var b strings.Builder
	for _, section := range []struct{ heading, body string }{
		{"## Ledger rules", rules},
		{"## Ledger stats", stats},
		{"## Steward learnings", learnings},
	} {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(section.heading + "\n")
		b.WriteString(section.body)
		if !strings.HasSuffix(section.body, "\n") {
			b.WriteString("\n")
		}
	}
	if b.Len() > stewardStartupLimit {
		return "", fmt.Errorf("ateam steward startup: output is %d bytes, over the %d-byte limit; condense steward learnings", b.Len(), stewardStartupLimit)
	}
	return b.String(), nil
}
