// peer.go — `ateam peer link`, the sanctioned way to pair two initiatives
// (agent-teams-q7rl.1 frozen items 4 and 5). It writes a "peer: <id>" line on
// each side through initiative.WithPeer and mails each side. The per-side
// helpers below are shared with `ateam peer unlink` and `ateam dispatch
// --cloud --peer`; they take the caller's verb prefix so error text names the
// command the user typed.
package verbs

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// RegisterPeerKong registers the `peer` parent verb onto p.
func RegisterPeerKong(p *cli.Parser) {
	p.AddVerb("peer", "Pair two initiatives so their DRIs know each other.", &peerCmd{})
}

// peerCmd is the kong parent struct for `ateam peer <subcommand>`. It has no
// Run method for the reason given on prCmd.
type peerCmd struct {
	Link peerLinkKong `cmd:"" name:"link" help:"Record peer: lines on two initiatives (laptop or cloud) and mail both."`
}

// peerLinkKong implements `ateam peer link <initiative-a> <initiative-b>`.
type peerLinkKong struct {
	A string `arg:"" name:"initiative-a" help:"First initiative id (laptop workspace or cloud_home)."`
	B string `arg:"" name:"initiative-b" help:"Second initiative id (laptop workspace or cloud_home)."`
}

// newPeerSend builds the sendKong that delivers a peer notice. Tests replace
// it to inject fakes for the delivery seams.
var newPeerSend = newSendKong

// peerSide is one resolved end of a pairing: the initiative, which store it
// lives in, and the runner for that store.
type peerSide struct {
	ID     string
	Kind   string // "laptop" or "cloud"
	Issue  bd.Issue
	Runner cli.BDRunner
}

const (
	peerKindLaptop = "laptop"
	peerKindCloud  = "cloud"
)

// peerRefuse checks what link, unlink, and dispatch --peer all refuse before
// touching any store: an id that is not an initiative id, and a hosted
// workspace. verb is the caller's error prefix (for example "ateam peer
// link"). Every refusal is a usage error.
func peerRefuse(ctx *cli.Context, verb string, ids ...string) error {
	for _, id := range ids {
		if !peerInitiativeID.MatchString(id) {
			return cli.Usagef("%s: %q is not a valid initiative id; nothing written", verb, id)
		}
	}
	if _, hosted := bd.HostedServer(ctx.Home); hosted {
		return cli.Usagef("%s: run this on a laptop; a cloud run cannot write a laptop initiative; nothing written", verb)
	}
	return nil
}

// resolvePeerSide finds id in the local workspace first, then in cloud_home
// when one is configured, and refuses a closed initiative. It writes nothing.
func resolvePeerSide(ctx *cli.Context, verb, id string) (peerSide, error) {
	issue, err := bd.ShowIssue(ctx.BD, id)
	side := peerSide{ID: id, Kind: peerKindLaptop, Issue: issue, Runner: ctx.BD}
	if err != nil {
		if !bd.IsNotFound(err) {
			return peerSide{}, fmt.Errorf("%s: read %s in this workspace: %w; nothing written", verb, id, err)
		}
		cloudHome, ok, cerr := bridgeCloudHome(ctx.Home)
		if cerr != nil {
			return peerSide{}, fmt.Errorf("%s: cloud_home config: %w; nothing written", verb, cerr)
		}
		if !ok {
			return peerSide{}, cli.Usagef("%s: initiative %s not found in this workspace; nothing written", verb, id)
		}
		runner := cloudSendBD(cloudHome)
		issue, err = bd.ShowIssue(runner, id)
		if err != nil {
			if bd.IsNotFound(err) {
				return peerSide{}, cli.Usagef("%s: initiative %s not found in this workspace or in cloud_home (%s); nothing written", verb, id, cloudHome)
			}
			return peerSide{}, fmt.Errorf("%s: read %s in cloud_home %s: %w; nothing written", verb, id, cloudHome, err)
		}
		side = peerSide{ID: id, Kind: peerKindCloud, Issue: issue, Runner: runner}
	}
	if side.Issue.Status == "closed" {
		return peerSide{}, cli.Usagef("%s: %s is closed; nothing written", verb, id)
	}
	return side, nil
}

// setPeerLine writes (add) or removes (!add) the "peer: <other>" line on side
// and confirms it with a second read of the same store. It reports changed
// when it wrote a new description; an unchanged description writes nothing and
// is not re-read. When the re-read disagrees (cloud_boot.go read-modify-writes
// the cloud description at boot and can replace a concurrent write) it returns
// an error carrying rerun, the command that repairs it.
func setPeerLine(side peerSide, other, verb, rerun string, add bool) (changed bool, err error) {
	edit := initiative.WithPeer
	if !add {
		edit = initiative.WithoutPeer
	}
	plan, err := edit(side.Issue, other)
	if err != nil {
		return false, fmt.Errorf("%s: %w", verb, err)
	}
	if plan.Description == side.Issue.Description {
		return false, nil
	}
	tmp, err := os.CreateTemp("", "ateam-peer-*.txt")
	if err != nil {
		return false, fmt.Errorf("%s: create temp file: %w", verb, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(plan.Description); err != nil {
		tmp.Close()
		return false, fmt.Errorf("%s: write temp file: %w", verb, err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("%s: write temp file: %w", verb, err)
	}
	if _, err := side.Runner.Run("update", side.ID, "--body-file="+tmp.Name()); err != nil {
		return false, fmt.Errorf("%s: bd update %s: %w", verb, side.ID, err)
	}
	reread, err := bd.ShowIssue(side.Runner, side.ID)
	if err != nil {
		return true, fmt.Errorf("%s: wrote the peer line on %s, but the re-read failed: %w; re-run: %s", verb, side.ID, err, rerun)
	}
	has := slices.Contains(initiative.Of(reread).Peers, other)
	if add && !has {
		return true, fmt.Errorf("%s: wrote peer: %s on %s, but a re-read does not show it (a concurrent description write may have replaced it); re-run: %s", verb, other, side.ID, rerun)
	}
	if !add && has {
		return true, fmt.Errorf("%s: removed peer: %s from %s, but a re-read still shows it; re-run: %s", verb, other, side.ID, rerun)
	}
	return true, nil
}

// peerLinkMessage is the frozen PEER LINK notice sent to self about other.
func peerLinkMessage(self, other peerSide) string {
	return fmt.Sprintf("PEER LINK: %s is paired with %s (%s initiative: %s).\n"+
		"It is listed as a peer: line in ateam show %s.\n"+
		"Coordinate with it directly on shared files, contracts, merge order, and anything one of you needs from the other. Mail it with: ateam mail send %s --file <msg-file>\n"+
		"Human gates still go through the steward (ateam gate).",
		self.ID, other.ID, other.Kind, cutTitle(other.Issue.Title, 80), self.ID, other.ID)
}

// notifyPeerSide mails body to side through sendKong in-process, so local
// delivery, resume, and the laptop-to-cloud run-bead comment are reused. prefix
// is the caller's output prefix (for example "peer link"). A cloud side with no
// where: run:<bead> line has not booted; it is skipped with a note, which is
// not a failure. It returns an error for any send failure and leaves the
// warning line to the caller.
func notifyPeerSide(ctx *cli.Context, prefix string, side peerSide, dedupKey, body string) error {
	if side.Kind == peerKindCloud && !strings.HasPrefix(initiative.Of(side.Issue).Where, whereRunPrefix) {
		fmt.Fprintf(ctx.Stdout, "%s: %s has no cloud run yet; it reads its peer: lines from ateam show when it boots\n", prefix, side.ID)
		return nil
	}
	tmp, err := os.CreateTemp("", "ateam-peer-msg-*.txt")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	send := newPeerSend()
	send.RecipientID = side.ID
	send.File = tmp.Name()
	send.DedupKey = dedupKey
	return send.Run(ctx)
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *peerLinkKong) Run(ctx *cli.Context) error {
	const verb = "ateam peer link"
	if ctx == nil {
		return fmt.Errorf("%s: no context", verb)
	}
	if c.A == c.B {
		return cli.Usagef("%s: %s and %s are the same initiative; nothing written", verb, c.A, c.B)
	}
	if err := peerRefuse(ctx, verb, c.A, c.B); err != nil {
		return err
	}
	a, err := resolvePeerSide(ctx, verb, c.A)
	if err != nil {
		return err
	}
	b, err := resolvePeerSide(ctx, verb, c.B)
	if err != nil {
		return err
	}

	rerun := fmt.Sprintf("ateam peer link %s %s", c.A, c.B)
	for _, p := range [][2]peerSide{{a, b}, {b, a}} {
		self, other := p[0], p[1]
		changed, err := setPeerLine(self, other.ID, verb, rerun, true)
		if err != nil {
			return err
		}
		if changed {
			fmt.Fprintf(ctx.Stdout, "peer link: recorded peer: %s on %s (%s)\n", other.ID, self.ID, self.Kind)
		} else {
			fmt.Fprintf(ctx.Stdout, "peer link: %s already lists peer: %s\n", self.ID, other.ID)
		}
	}

	failed := false
	for _, p := range [][2]peerSide{{a, b}, {b, a}} {
		self, other := p[0], p[1]
		if err := notifyPeerSide(ctx, "peer link", self, "peer-link-"+other.ID, peerLinkMessage(self, other)); err != nil {
			fmt.Fprintf(ctx.Stderr, "peer link: warning: could not notify %s: %v\n", self.ID, err)
			failed = true
		}
	}
	if failed {
		return fmt.Errorf("%s: both peer: lines are recorded, but a notice was not sent; re-run: %s", verb, rerun)
	}
	return nil
}
