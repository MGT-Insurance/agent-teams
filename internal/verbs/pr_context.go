// pr_context.go — `ateam pr context <initiative>`, owned by rm8 T6
// (agent-teams-lrj.7). The output shape is frozen byte-for-byte in
// docs/outsider-requests.md ("Multi-PR context block"); this file only wires
// the slots the contract leaves open from the initiative fields, the resolved
// PR list and the gate notes. Read-only: no bd writes.
package verbs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// prContextKong implements `ateam pr context <initiative-id> [--pr <url>]`.
type prContextKong struct {
	InitiativeID string `arg:"" name:"initiative-id" help:"Initiative ID whose PR list the context block is built from."`
	PR           string `name:"pr" help:"Print only the block for this PR (a PR recorded on the initiative). Default: one block per recorded PR, separated by a blank line."`
	MergeOrder   string `name:"merge-order" help:"Comma list of recorded PRs as <owner>/<repo>#<number>, in the order they merge. The DRI decides it; the Merge order line is printed only when this is given and the initiative has more than one PR."`
}

const (
	prContextOpen   = "<!-- a-team-context -->"
	prContextClose  = "<!-- /a-team-context -->"
	prContextReview = "Review: @jbarneson is required. PO comments are advice."
)

var (
	prContextURLRE    = regexp.MustCompile(`https://\S+`)
	prContextClassRE  = regexp.MustCompile(`(?m)^class:[ \t]*plan[ \t]*$`)
	prContextDecision = regexp.MustCompile(`(?m)^decision:[ \t]*(.*)$`)
)

// Run satisfies the kong runner interface.
func (c *prContextKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam pr context: no context")
	}
	issue, err := bd.ShowIssue(ctx.BD, c.InitiativeID)
	if err != nil {
		return fmt.Errorf("ateam pr context: bd show %s: %w", c.InitiativeID, err)
	}
	prs := initiative.ResolvedPRs(issue)
	if len(prs) == 0 {
		return fmt.Errorf("ateam pr context: initiative %s has no recorded PR (record one with ateam pr add)", c.InitiativeID)
	}
	f := initiative.Of(issue)
	outsider := f.Origin == initiative.OriginOutsider
	if outsider && (f.Request == "" || f.Requester == "") {
		return fmt.Errorf("ateam pr context: outsider initiative %s is missing its request or requester field", c.InitiativeID)
	}

	only := -1
	if c.PR != "" {
		canon, ok := initiative.CanonicalPRURL(c.PR)
		if !ok {
			return cli.Usagef("ateam pr context: --pr must be a full GitHub PR URL (https://github.com/<owner>/<repo>/pull/<number>), got %q", c.PR)
		}
		for i, p := range prs {
			if p == canon {
				only = i
			}
		}
		if only < 0 {
			return cli.Usagef("ateam pr context: --pr %s is not a PR recorded on %s (recorded PRs: %v)", c.PR, c.InitiativeID, prs)
		}
	}

	var order []string
	if c.MergeOrder != "" {
		known := map[string]bool{}
		for _, p := range prs {
			s, _ := prRefAndNumber(p)
			known[s] = true
		}
		seen := map[string]bool{}
		for _, r := range strings.Split(c.MergeOrder, ",") {
			r = strings.TrimSpace(r)
			if !known[r] {
				return cli.Usagef("ateam pr context: --merge-order entry %q is not a PR recorded on %s as <owner>/<repo>#<number>", r, c.InitiativeID)
			}
			if seen[r] {
				return cli.Usagef("ateam pr context: --merge-order lists %q twice", r)
			}
			seen[r] = true
			order = append(order, r)
		}
	}

	planURL := latestPlanURL(issue.Notes)
	ref := f.Request
	if f.Linear != "" {
		ref = "on Linear " + f.Linear
	}

	var blocks []string
	for i := range prs {
		if only >= 0 && i != only {
			continue
		}
		blocks = append(blocks, buildPRContextBlock(prs, i, outsider, ref, f.Requester, planURL, order))
	}
	fmt.Fprint(ctx.Stdout, strings.Join(blocks, "\n"))
	return nil
}

// prRefAndNumber splits a PR URL into its "<owner>/<repo>#<n>" summary and its
// number. A URL that does not parse is used verbatim.
func prRefAndNumber(url string) (summary, number string) {
	if m := initiative.PRURLRE.FindStringSubmatch(url); m != nil {
		return m[1] + "/" + m[2] + "#" + m[3], m[3]
	}
	return url, url[strings.LastIndex(url, "/")+1:]
}

// buildPRContextBlock renders the frozen block for prs[k]. Every line ends
// with a newline.
func buildPRContextBlock(prs []string, k int, outsider bool, ref, requester, planURL string, order []string) string {
	n := len(prs)
	var b strings.Builder
	b.WriteString(prContextOpen + "\n")
	if outsider {
		fmt.Fprintf(&b, "Part %d of %d for the request %s (asked by %s).\n", k+1, n, ref, requester)
	} else {
		fmt.Fprintf(&b, "Part %d of %d.\n", k+1, n)
	}
	this, _ := prRefAndNumber(prs[k])
	fmt.Fprintf(&b, "This PR: %s.", this)
	var others, nums []string
	for i, p := range prs {
		if i == k {
			continue
		}
		s, num := prRefAndNumber(p)
		others = append(others, s)
		nums = append(nums, "#"+num)
	}
	switch len(others) {
	case 0:
		b.WriteString("\n")
	case 1:
		fmt.Fprintf(&b, " The other PR (%s): %s.\n", nums[0], others[0])
	default:
		fmt.Fprintf(&b, " The other PRs (%s): %s.\n", strings.Join(nums, ", "), strings.Join(others, "; "))
	}
	if n > 1 && len(order) > 0 {
		b.WriteString("Merge order: " + strings.Join(order, " then ") + "\n")
	}
	if planURL != "" {
		b.WriteString("Plan: " + planURL + "\n")
	}
	if outsider {
		b.WriteString(prContextReview + "\n")
	}
	b.WriteString(prContextClose + "\n")
	return b.String()
}

// latestPlanURL returns the first https URL of the LAST plan-approval ask block
// in the gate notes, or "". A block is a plan approval when its decision
// matches planGateMarker or it carries a "class: plan" line. Prose-form gate
// notes (no ask block) are not scanned.
func latestPlanURL(notes string) string {
	url := ""
	for rest := notes; ; {
		i := strings.Index(rest, "<<<ateam-ask")
		if i < 0 {
			break
		}
		rest = rest[i:]
		end := strings.Index(rest, ">>>")
		if end < 0 {
			end = len(rest)
		}
		block := rest[:end]
		rest = rest[end:]
		isPlan := prContextClassRE.MatchString(block)
		if m := prContextDecision.FindStringSubmatch(block); m != nil && planGateMarker.MatchString(strings.TrimSpace(m[1])) {
			isPlan = true
		}
		if !isPlan {
			continue
		}
		if u := prContextURLRE.FindString(block); u != "" {
			url = strings.TrimRight(u, ".,;)")
		}
	}
	return url
}
