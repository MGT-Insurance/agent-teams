package verbs

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

func TestPoCharterCheckRun(t *testing.T) {
	var out bytes.Buffer
	ctx := &cli.Context{Stdout: &out}
	if err := poCharterCheckRun(ctx, &poCharterCheckKong{File: "../po/testdata/sample-charter.md"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "OK area=quoting") {
		t.Fatalf("out = %q", out.String())
	}

	bad := t.TempDir() + "/c.md"
	writeFileT(t, bad, "---\narea: x\nsignals: []\n---\n")
	out.Reset()
	err := poCharterCheckRun(ctx, &poCharterCheckKong{File: bad})
	if cli.ExitCode(err) != 1 || !strings.Contains(out.String(), "goal-required:") {
		t.Fatalf("err=%v out=%q", err, out.String())
	}
}
