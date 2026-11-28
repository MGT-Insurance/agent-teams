// Package po holds the FROZEN contract for agentic product owners (POs).
//
// Bead agent-teams-9yj.1 freezes the shapes below so the PO tracks
// (9yj.2 charter check, 9yj.3 state, 9yj.4 ledger, 9yj.5 startup, 9yj.6 skill,
// 9yj.10.x, 9yj.11 and the Dispatcher handoffs 9yj.8, .9, .12) can run in
// parallel and file-disjoint. Changing anything here needs the planner and
// team-lead, never a sibling track. This package declares types and constants
// only; it has no logic.
//
// With no PO defined, nothing in agent-teams changes: no extra gates, no new
// required fields, no new notifications, and every existing ateam verb prints
// what it printed before. The only additions are the "po" command group, the
// "po" skill and the types here.
//
// # 1. Charter format
//
// A charter is a markdown file with YAML front matter. The prose after the
// front matter is context only; nothing parses it. Location:
// users/eric.lloyd/po-charters/<area>.md. Front matter keys (see Charter in
// types.go):
//
//	area        kebab-case area name
//	mission     one-paragraph mission
//	repos[]     repos the area covers
//	features[]  {id, name, match{words[], repos[], paths[], services[]}}
//	goals[]     {id, title, done, signals[] (signal ids)}
//	signals[]   {id, title, source: warehouse|datadog|linear|langfuse|vercel,
//	            query, target?{op: >=|<=, value}}
//	fences[]    {what, reason}
//	reach       {topic_title, summary: bool (default false)}
//
// Validation rules (checked by `ateam po charter check`, bead 9yj.2):
//
//   - at least one goal;
//   - every goal signal id resolves to a signals[] entry;
//   - no task backlog key (a charter is not a task list);
//   - ids are unique (features, goals and signals each);
//
// Rulings (Eric): no separate area-scoping field beyond the match rules; judgment
// over timers.
//
// # 2. PO agent definition (consumed by the Dispatcher handoff bead 9yj.8)
//
// One Dispatcher agent definition per PO, mirroring steward.md:
//
//	file             users/eric.lloyd/agents/po-<area>.md
//	model            anthropic/claude-opus (exact id per the Dispatcher catalog)
//	budget_usd_per_day  20 (advisory, adjustable per PO)
//	thread           continuous
//	schedule         safety net, as guidance: 15 min while work is in flight,
//	                 hourly otherwise; the PO may override
//	prompt_command   ateam po startup <area>
//	cli_allow        ateam po startup, ateam po state, ateam po charter check,
//	                 ateam list, ateam show, ateam learn po,
//	                 ateam learn po-<area>,
//	                 ateam steward ledger record/stats/recall --persona po-<area>
//	charter          users/eric.lloyd/po-charters/<area>.md
//
// One PO Telegram group with one topic per PO on the existing bot (topic
// title from the charter reach.topic_title).
//
// # 3. Per-PO state and ledger keys (hosted store)
//
//	po:<area>:claim:<initiative-id>  -> {fit: clear|possible, reason, at, lead: bool}
//	po:<area>:said:<initiative-id>   -> [{to: dri|steward|eric, at, gist}]
//	po:<area>:reading:<feature-id>:<signal-id>:<rfc3339> -> {value, source, note}
//	po:<area>:view                   -> markdown
//
// Ledger key prefix: poledger.<area>. (the steward stays stewardledger.).
//
// New ledger categories: nudge, impact-call, new-work, bold-proposal,
// overlap-call, charter-change, claim, plan-feedback, pr-fit.
//
// Learnings roles: po (shared by every PO) and po-<area>.
//
// # 4. Frozen comment prefix
//
// Every PO-authored comment on an initiative or PR starts with exactly:
//
//	po: note (not the owner's words):
//
// Readers also accept the legacy "po: note (not Eric's words):" (CommentPrefix).
//
// # 5. Command group (internal/verbs/po_kong.go)
//
// Kong group "po": charter check, state get|set|list, startup. Each Run
// delegates to a function in its own file (po_charter_run.go, po_state_run.go,
// po_startup_run.go); until the owning bead lands the function returns
// "not implemented".
//
// # 6. Frozen skill layout
//
//	promptsrc/agent-teams/skills/po/SKILL.md          owned by 9yj.6; links
//	promptsrc/agent-teams/skills/po/references/vision.md      (9yj.10.2)
//	promptsrc/agent-teams/skills/po/references/proposals.md   (9yj.10.2)
//	promptsrc/agent-teams/skills/po/references/plans-and-prs.md (9yj.11)
//
// The manifest entry skill.po in promptsrc/agent-teams/manifests/skills.json
// is added by this bead; 9yj.6 owns the SKILL.md body (this bead ships a
// placeholder body so prompt-sync check passes).
//
// # 7. PO-to-steward proposal mail
//
// A mail bead assigned to agent:steward whose body starts with the header
// line "kind: po-proposal", followed by the fields repo:, problem:, goal:,
// evidence: and po: (one per line), then a line "---", then free text. The
// steward side is bead 9yj.10.3.//
// # 8. Stamp line
//
// The first line of every PO note, review body and relayed note names its
// writer (see stamp.go; FormatStamp and ParseStamp are the only writer and
// reader):
//
//	po: note (not the owner's words): from <owner>/<agent> (internal|external)
//
// It starts with NeutralCommentPrefix byte for byte; readers also accept the
// legacy CommentPrefix ("not Eric's words"). A note with no stamp (the bare prefix) is
// a legacy note whose source is unknown: ParseStamp returns ok=false, never an
// error. <owner> is the Dispatcher handle and <agent> the agent name; each is
// lowercase alphanumeric tokens joined by "." or "-". A relayed external note
// gets a LAST line "relayed-by: steward (writer peer:<owner>/<agent>)"
// (RelayedByPrefix). A review body has line 2 "Value check: fits" or
// "Value check: does not fit", and a request-changes review may add line 3
// "Request changes: extreme drift" or "Request changes: one-way door".
package po
