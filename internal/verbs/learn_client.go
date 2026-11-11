package verbs

import (
	"fmt"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// NewLearnClient returns the bd client the role and user learnings verbs use.
// With cloud_home unset, or naming the same workspace as home, it returns
// local itself, so ctx.Learn == ctx.BD and every learnings verb sends the same
// bd argv to the same home as before. With cloud_home naming another
// workspace it returns a client bound to that hosted workspace. A malformed
// config.toml yields a runner whose every call fails.
func NewLearnClient(home string, local cli.BDRunner) cli.BDRunner {
	cloudHome, ok, err := workspaceconfig.CloudHome(home)
	if err != nil {
		return &errRunner{err: fmt.Errorf("ateam: learnings store: %w", err)}
	}
	if !ok || sameWorkspace(home, cloudHome) {
		return local
	}
	return bd.NewClientWithEnv(cloudHome, hostedTLSEnv)
}

// errRunner is a cli.BDRunner whose every call returns err.
type errRunner struct{ err error }

func (e *errRunner) Run(...string) (string, error) { return "", e.err }

func (e *errRunner) RunJSON(any, ...string) error { return e.err }

// learnBD returns the client for role and user learnings: ctx.Learn when set,
// otherwise ctx.BD (tests and the pre-initialization path leave Learn nil).
func learnBD(ctx *cli.Context) cli.BDRunner {
	if ctx.Learn != nil {
		return ctx.Learn
	}
	return ctx.BD
}

// learnRemote reports whether learnings are routed to a workspace other than
// the local one.
func learnRemote(ctx *cli.Context) bool {
	return ctx.Learn != nil && ctx.Learn != ctx.BD
}

// isStoreUnreachable reports whether err means the Dolt server could not be
// reached: bd's "Dolt server unreachable" text, or the go-sql-driver and net
// texts bd passes through when a hosted connection dies ("invalid connection",
// "i/o timeout", "connection refused", "no such host").
func isStoreUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, s := range []string{"Dolt server unreachable", "invalid connection", "i/o timeout", "connection refused", "no such host"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// learnStoreName returns the configured cloud_home text used in unreachable
// messages.
func learnStoreName(ctx *cli.Context) string {
	cloudHome, _, _ := workspaceconfig.CloudHome(ctx.Home)
	return cloudHome
}

// learnUnreachable reports whether err is an unreachable hosted store for a
// routed call. With learnings kept local it is always false, so today's error
// path is unchanged.
func learnUnreachable(ctx *cli.Context, err error) bool {
	return learnRemote(ctx) && isStoreUnreachable(err)
}

// learnReadFailure maps a failed non-serving read (recall, roles,
// memories-json, condense-check, condense) to the unreachable error, or
// returns err unchanged.
func learnReadFailure(ctx *cli.Context, verb string, err error) error {
	if learnUnreachable(ctx, err) {
		return fmt.Errorf("ateam %s: learnings store unreachable (%s)", verb, learnStoreName(ctx))
	}
	return err
}

// learnWriteFailure maps a failed write (learn, forget, applied, fresh-drain)
// to the NOT saved error, or returns err unchanged.
func learnWriteFailure(ctx *cli.Context, verb string, err error) error {
	if learnUnreachable(ctx, err) {
		return fmt.Errorf("ateam %s: learnings store unreachable (%s): NOT saved; retry later", verb, learnStoreName(ctx))
	}
	return err
}

// freshDrainFailure is learnWriteFailure for fresh-drain: the unreachable error
// also reports how many keys moved before the first failed call.
func freshDrainFailure(ctx *cli.Context, moved int, err error) error {
	if learnUnreachable(ctx, err) {
		return fmt.Errorf("ateam fresh-drain: learnings store unreachable (%s): NOT saved; retry later (moved %d keys)", learnStoreName(ctx), moved)
	}
	return err
}

// learnServeUnreachable handles a failed read that serves an agent (learnings,
// prime). When the hosted store is unreachable it prints the continue-without
// line to stdout and reports true, so the caller exits 0.
func learnServeUnreachable(ctx *cli.Context, who string, err error) bool {
	if !learnUnreachable(ctx, err) {
		return false
	}
	fmt.Fprintf(ctx.Stdout, "[learnings store unreachable (%s): continuing WITHOUT %s learnings]\n", learnStoreName(ctx), who)
	return true
}
