package verbs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/po"
)

// poStartupLimit is the most bytes `ateam po startup` prints; the same bound
// as stewardStartupLimit, for the same Dispatcher cli_run cap.
const poStartupLimit = stewardStartupLimit

// poStartupViewLimit is the most bytes of the PO's view the startup text
// carries. A longer view is cut and the text says how to read the rest.
const poStartupViewLimit = 16 * 1024

// poStartupNow is the clock the view age is measured against; tests replace it.
var poStartupNow = time.Now

// poRevisedPrefix is the line a view may start with to date itself:
// "revised: <RFC3339>". The state store keeps no write time, so the age comes
// from this line.
const poRevisedPrefix = "revised:"

// poStartupRulesIntro leads the shared ledger rules section.
const poStartupRulesIntro = "These ledger rules are shared with the steward. A PO passes --persona po-<area> on every `ateam steward ledger` call (record, stats, recall) so its rows stay apart from the steward's; the categories are the steward's plus nudge, impact-call, new-work, bold-proposal, overlap-call, charter-change, claim, plan-feedback and pr-fit."

// poStartupRun implements `ateam po startup <area>`. Like the steward's, it
// prints all of its text or nothing: an unreachable store or output over the
// limit is an error with nothing on stdout. A missing or invalid charter is not
// an error; the text opens with a drafting-mode banner instead.
func poStartupRun(ctx *cli.Context, c *poStartupKong) error {
	if ctx == nil {
		return fmt.Errorf("ateam po startup: nil context")
	}
	prefix, err := po.KeyPrefix(c.Area)
	if err != nil {
		return cli.Usagef("ateam po startup: %v", err)
	}
	store, err := poStateStoreOf(ctx, "startup")
	if err != nil {
		return fmt.Errorf("ateam po startup: %w", err)
	}

	charter := poStartupCharter(c)
	view, ok, err := po.Get(store, c.Area, po.KindView)
	if err != nil {
		return fmt.Errorf("ateam po startup: %w", err)
	}
	claims, err := store.List(prefix + po.KindClaim + ":")
	if err != nil {
		return fmt.Errorf("ateam po startup: %w", err)
	}
	persona := stewardLedgerPOPersonaPrefix + c.Area
	stats, err := captureStatsFor(ctx, persona)
	if err != nil {
		return err
	}
	shared, err := capturePOLearnings(ctx, po.RoleShared)
	if err != nil {
		return err
	}
	own, err := capturePOLearnings(ctx, persona)
	if err != nil {
		return err
	}

	sections := []struct{ heading, body string }{
		{"## Charter", charter},
		{"## View", poStartupViewText(view, ok, poStartupNow())},
		{"## Claims", poStartupClaimsText(prefix, claims)},
		{"## Ledger rules", poStartupRulesIntro + "\n\n" + stewardSharedLedgerRules},
		{"## Ledger stats (" + persona + ")", stats},
		{"## PO learnings (shared, role " + po.RoleShared + ")", shared},
		{"## PO learnings (" + persona + ")", own},
	}
	var b strings.Builder
	for _, s := range sections {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s.heading + "\n" + s.body)
		if !strings.HasSuffix(s.body, "\n") {
			b.WriteString("\n")
		}
	}
	if b.Len() > poStartupLimit {
		return fmt.Errorf("ateam po startup: output is %d bytes, over the %d-byte limit; condense the PO learnings", b.Len(), poStartupLimit)
	}
	_, err = io.WriteString(ctx.Stdout, b.String())
	return err
}

// poStartupCharter reads and validates the charter and returns its section
// text: the full summary when valid, otherwise the drafting-mode banner.
func poStartupCharter(c *poStartupKong) string {
	path := c.Charter
	if path == "" {
		path = "users/eric.lloyd/po-charters/" + c.Area + ".md"
	}
	banner := func(why string) string {
		return "DRAFTING MODE: " + why + "\nUntil a valid charter is merged, read and ask only: no claims, no nudges, no comments on initiatives or PRs, no new work. Say so in your topic and ask Eric for the charter.\n"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return banner(fmt.Sprintf("charter %s is not readable (%v).", path, err))
	}
	ch, viol := po.CheckCharter(data)
	if len(viol) > 0 {
		var b strings.Builder
		b.WriteString(banner(fmt.Sprintf("charter %s is invalid.", path)))
		for _, v := range viol {
			fmt.Fprintf(&b, "- %s: %s\n", v.Rule, v.Msg)
		}
		return b.String()
	}
	if ch.Area != c.Area {
		return banner(fmt.Sprintf("charter %s is for area %q, not %q.", path, ch.Area, c.Area))
	}
	return poCharterText(ch, path)
}

// poCharterText renders a valid charter: goals, signals, fences and features.
func poCharterText(ch *po.Charter, path string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Charter %s (valid): %s\n", path, po.Summary(ch))
	fmt.Fprintf(&b, "Mission: %s\n", strings.TrimSpace(ch.Mission))
	if len(ch.Repos) > 0 {
		fmt.Fprintf(&b, "Repos: %s\n", strings.Join(ch.Repos, ", "))
	}
	b.WriteString("Features:\n")
	for _, f := range ch.Features {
		fmt.Fprintf(&b, "- %s (%s)", f.ID, f.Name)
		m := f.Match
		for _, l := range []struct {
			name string
			v    []string
		}{{"words", m.Words}, {"repos", m.Repos}, {"paths", m.Paths}, {"services", m.Services}} {
			if len(l.v) > 0 {
				fmt.Fprintf(&b, " %s=[%s]", l.name, strings.Join(l.v, ", "))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("Goals:\n")
	for _, g := range ch.Goals {
		fmt.Fprintf(&b, "- %s: %s; done when: %s; signals: %s\n", g.ID, g.Title, g.Done, strings.Join(g.Signals, ", "))
	}
	b.WriteString("Signals:\n")
	for _, s := range ch.Signals {
		fmt.Fprintf(&b, "- %s: %s (%s) query: %s", s.ID, s.Title, s.Source, strings.TrimSpace(s.Query))
		if s.Target != nil {
			fmt.Fprintf(&b, " target: %s %g", s.Target.Op, s.Target.Value)
		}
		b.WriteString("\n")
	}
	b.WriteString("Fences (never do these):\n")
	for _, f := range ch.Fences {
		fmt.Fprintf(&b, "- %s (%s)\n", f.What, f.Reason)
	}
	fmt.Fprintf(&b, "Reach: topic %q, summary %t\n", ch.Reach.TopicTitle, ch.Reach.Summary)
	return b.String()
}

// poStartupViewText renders the view with its age. A view that starts with a
// "revised: <RFC3339>" line is dated from it; any other view is shown as
// stored with its age marked unknown.
func poStartupViewText(view string, ok bool, now time.Time) string {
	if !ok || strings.TrimSpace(view) == "" {
		return "No view yet. Write one with `ateam po state set <area> view`, starting it with a line `revised: <RFC3339 time>`.\n"
	}
	age := "view age unknown (no leading `revised: <RFC3339>` line)"
	first, _, _ := strings.Cut(view, "\n")
	if rest, found := strings.CutPrefix(strings.TrimSpace(first), poRevisedPrefix); found {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(rest)); err == nil {
			age = "view last revised " + poAgeText(now.Sub(t))
		}
	}
	text := view
	if len(text) > poStartupViewLimit {
		text = text[:poStartupViewLimit] + "\n[view cut at " + fmt.Sprint(poStartupViewLimit) + " bytes; read the rest with `ateam po state get <area> view`]\n"
	}
	return age + "\n\n" + text
}

// poAgeText renders a duration as a coarse "N days ago".
func poAgeText(d time.Duration) string {
	switch {
	case d < 0:
		return "in the future (clock skew)"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

// poStartupClaimsText lists the PO's claims, one line per initiative.
func poStartupClaimsText(prefix string, rows map[string]string) string {
	if len(rows) == 0 {
		return "No claims.\n"
	}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "- %s: %s\n", strings.TrimPrefix(k, prefix+po.KindClaim+":"), strings.Join(strings.Fields(rows[k]), " "))
	}
	return b.String()
}

// captureStatsFor returns `ateam steward ledger stats --persona <persona>`.
func captureStatsFor(ctx *cli.Context, persona string) (string, error) {
	var out bytes.Buffer
	inner := *ctx
	inner.Stdout = &out
	if err := (&stewardLedgerStatsKong{Persona: persona}).Run(&inner); err != nil {
		return "", fmt.Errorf("ateam po startup: %w", err)
	}
	return out.String(), nil
}

// capturePOLearnings returns `ateam learnings <role>`; an unreachable store is
// an error, as in the steward's startup.
func capturePOLearnings(ctx *cli.Context, role string) (string, error) {
	var out bytes.Buffer
	inner := *ctx
	inner.Stdout = &out
	unreachable, err := serveLearnings(&inner, role)
	if err != nil {
		return "", fmt.Errorf("ateam po startup: %w", err)
	}
	if unreachable {
		return "", fmt.Errorf("ateam po startup: learnings store unreachable (%s)", learnStoreName(ctx))
	}
	return out.String(), nil
}
