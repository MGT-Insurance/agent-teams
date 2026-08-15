// Package verbs: the unified `ateam mail` parent verb (send/inbox/list/
// close/purge) and its 3 hidden deprecated aliases (send, inbox, debug-mail).
// This is the ONE place the parent verb is wired, since it references leaf
// types from both messaging.go and mail.go — a coupled unit that would
// collide across files if split further. See bead agent-teams-790o.1 for the
// frozen contract.
// File owned by the GO track (agent-teams-790o.2).
package verbs

import (
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// mailCmd is the kong parent struct for `ateam mail <subcommand>`. It has NO
// Run method: kong's kctx.Run walks the selected leaf up through its parents
// and runs every node that has a Run method, so a Run here would fire on
// every subcommand.
type mailCmd struct {
	Send  sendKong      `cmd:"" name:"send"  help:"Send a message to a recipient initiative."`
	Inbox inboxKong     `cmd:"" name:"inbox" help:"Read and consume unread messages for this initiative."`
	List  mailListKong  `cmd:"" name:"list"  help:"List recent cross-initiative mail (all statuses, including closed)."`
	Close mailCloseKong `cmd:"" name:"close" help:"Close a message bead."`
	Purge mailPurgeKong `cmd:"" name:"purge" help:"Permanently delete old closed message beads."`
}

// ── hidden deprecated aliases ────────────────────────────────────────────────
//
// The old flat verbs (send/inbox/debug-mail) still work — human muscle
// memory, stored role-learnings, and not-yet-updated installed-plugin hooks
// all still call the old names — but are hidden from --help and print a
// deprecation note to stderr only, so stdout stays clean for JSON consumers
// (e.g. `ateam debug-mail --json`, which the dashboard parses).

// sendAliasKong embeds sendKong (anonymous field: kong flattens its
// flags/args) and overrides Run to warn then delegate.
type sendAliasKong struct {
	sendKong
}

func (c *sendAliasKong) Run(ctx *cli.Context) error {
	fmt.Fprintln(ctx.Stderr, "note: ateam send is deprecated; use ateam mail send.")
	return c.sendKong.Run(ctx)
}

// inboxAliasKong embeds inboxKong and overrides Run to warn then delegate.
type inboxAliasKong struct {
	inboxKong
}

func (c *inboxAliasKong) Run(ctx *cli.Context) error {
	fmt.Fprintln(ctx.Stderr, "note: ateam inbox is deprecated; use ateam mail inbox.")
	return c.inboxKong.Run(ctx)
}

// mailListAliasKong embeds mailListKong and overrides Run to warn then
// delegate. Registered under the old "debug-mail" name.
type mailListAliasKong struct {
	mailListKong
}

func (c *mailListAliasKong) Run(ctx *cli.Context) error {
	fmt.Fprintln(ctx.Stderr, "note: ateam debug-mail is deprecated; use ateam mail list.")
	return c.mailListKong.Run(ctx)
}

// RegisterMailKong registers the unified `mail` parent verb and the 3 hidden
// deprecated aliases (send, inbox, debug-mail) onto p.
func RegisterMailKong(p *cli.Parser) {
	p.AddVerb("mail", "Send, read, list, close, and purge cross-initiative mail.", &mailCmd{
		Send: sendKong{
			agentsFunc:     defaultAgentsJSONAll,
			resumeFunc:     defaultResume,
			codexWake:      defaultCodexWake,
			sleeper:        defaultSleeper,
			doorbellExists: defaultDoorbellExists,
			respawnFunc:    defaultRespawn,
		},
	})

	p.AddHiddenVerb("send", "Deprecated: use `ateam mail send`.", &sendAliasKong{
		sendKong: sendKong{
			agentsFunc:     defaultAgentsJSONAll,
			resumeFunc:     defaultResume,
			codexWake:      defaultCodexWake,
			sleeper:        defaultSleeper,
			doorbellExists: defaultDoorbellExists,
			respawnFunc:    defaultRespawn,
		},
	})
	p.AddHiddenVerb("inbox", "Deprecated: use `ateam mail inbox`.", &inboxAliasKong{})
	p.AddHiddenVerb("debug-mail", "Deprecated: use `ateam mail list`.", &mailListAliasKong{})
}
