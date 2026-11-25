// gate_class.go — `ateam gate --class` and the clear-gate class cleanup
// (rm8 T2, agent-teams-lrj.3). Contract: docs/outsider-requests.md ("Gate
// class").
package verbs

import (
	"strings"
	"sync"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// gateClassLabelPrefix prefixes the label that records a gate's class. It is
// deliberately not "gate:" so no reader of the "gate:" label family matches it.
const gateClassLabelPrefix = "gate-class:"

// gateMailClass maps an initiative id to the class its gate mail must carry.
// gateClassApply sets it and newGateMailOp reads it: newGateMailOp has no
// context, and its caller in kong_converted.go is frozen.
var (
	gateMailClassMu sync.Mutex
	gateMailClass   = map[string]string{}
)

func setGateMailClass(id, class string) {
	gateMailClassMu.Lock()
	defer gateMailClassMu.Unlock()
	gateMailClass[id] = class
}

// gateMailClassLine returns "class: <class>\n" when gateClassApply registered
// a class for id, else "".
func gateMailClassLine(id string) string {
	gateMailClassMu.Lock()
	defer gateMailClassMu.Unlock()
	if c := gateMailClass[id]; c != "" {
		return "class: " + c + "\n"
	}
	return ""
}

// resolveGateClass maps a gate to its class: an explicit --class wins; else
// "question" with --plan is plan, "question" is scope, "live-test-review" is
// live-test, "review" is delivery. Other kinds have no class.
func resolveGateClass(explicit, kind string, plan bool) string {
	if explicit != "" {
		return explicit
	}
	switch kind {
	case "question":
		if plan {
			return "plan"
		}
		return "scope"
	case "live-test-review":
		return "live-test"
	case "review":
		return "delivery"
	}
	return ""
}

// gateClassApply runs at the top of every `ateam gate`. It does not call bd
// itself: it wraps ctx.BD so that right AFTER the gate's own labelling update
// (the call that adds "human") the class is applied. That keeps the gate's own
// bd call sequence unchanged; the class work appends calls at the end. A gate
// outside a Dispatcher run with no --class does nothing. The "gate-class:"
// label is written when --class is given or the initiative is an outsider;
// the "class:" mail line only for an outsider, and only inside a Dispatcher run (it lives in the outbox mail).
func gateClassApply(ctx *cli.Context, c *gateKong) error {
	_, _, inRun := dispatcherRun()
	if c.Class == "" && !inRun {
		return nil
	}
	ctx.BD = &gateClassAfterGate{inner: ctx.BD, ctx: ctx, gate: c, inRun: inRun}
	return nil
}

type gateClassAfterGate struct {
	inner cli.BDRunner
	ctx   *cli.Context
	gate  *gateKong
	inRun bool
	done  bool
}

func (g *gateClassAfterGate) RunJSON(dst any, args ...string) error {
	return g.inner.RunJSON(dst, args...)
}

func (g *gateClassAfterGate) Run(args ...string) (string, error) {
	out, err := g.inner.Run(args...)
	if err != nil || g.done || !isGateLabelUpdate(args) {
		return out, err
	}
	g.done = true
	return out, g.apply()
}

// isGateLabelUpdate reports whether args is the gate's `bd update <id> ...
// --add-label human ...` call.
func isGateLabelUpdate(args []string) bool {
	if len(args) == 0 || args[0] != "update" {
		return false
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--add-label" && args[i+1] == "human" {
			return true
		}
	}
	return false
}

func (g *gateClassAfterGate) apply() error {
	c := g.gate
	special := false
	if g.inRun {
		iss, err := bd.ShowIssue(g.inner, c.ID)
		if err != nil {
			if c.Class == "" {
				return nil // cannot tell: behave as a non-outsider gate
			}
		} else {
			f := initiative.Of(iss)
			special = f.Origin == initiative.OriginOutsider
		}
	}
	class := resolveGateClass(c.Class, c.Kind, c.Plan)
	if class == "" || (c.Class == "" && !special) {
		return nil
	}
	if special {
		setGateMailClass(c.ID, class)
	}
	_, err := g.inner.Run("update", c.ID, "--add-label", gateClassLabelPrefix+class)
	return err
}

// gateClassClear arranges for clear-gate to drop gate-class:* labels without
// an extra bd call: it wraps ctx.BD so that when a `bd show` during this clear
// reports such labels, the next `bd update <id>` also removes them. A clear on
// a gate with no gate-class label sees identical bd calls to before.
func gateClassClear(ctx *cli.Context, c *clearGateKong) error {
	ctx.BD = &gateClassCleaner{inner: ctx.BD}
	return nil
}

type gateClassCleaner struct {
	inner   cli.BDRunner
	pending []string
}

func (g *gateClassCleaner) RunJSON(dst any, args ...string) error {
	return g.inner.RunJSON(dst, args...)
}

func (g *gateClassCleaner) Run(args ...string) (string, error) {
	if len(args) > 0 && args[0] == "update" && len(g.pending) > 0 {
		args = append(append([]string{}, args...), g.pending...)
		g.pending = nil
	}
	out, err := g.inner.Run(args...)
	if err == nil && len(args) > 0 && args[0] == "show" {
		if iss, e := bd.ShowIssue(staticOut(out), ""); e == nil {
			for _, l := range iss.Labels {
				if strings.HasPrefix(l, gateClassLabelPrefix) {
					g.pending = append(g.pending, "--remove-label", l)
				}
			}
		}
	}
	return out, err
}

// staticOut lets bd.ShowIssue parse output already fetched.
type staticOut string

func (s staticOut) Run(args ...string) (string, error) { return string(s), nil }
