// Package sentlog implements the append-only audit log of every message
// sent to Eric via the configured transport, plus the schema and helpers
// `ateam sent` uses to read it back.
//
// This exists because the steward once told Eric "that wasn't me" about a
// Telegram message and was wrong, with no way to check — its context had
// been compacted and the sent messages went with it. Eric ruled the visible
// feed stays byte-for-byte unchanged ("keep as it is today"), so this log is
// now the ONLY mechanism that will ever answer "was that you?"
// (agent-teams-48dh, contract agent-teams-48dh.1).
//
// Every field name, type, path, and constant in this package is FROZEN by
// that contract. Do not rename, retype, or reorder anything here without an
// amendment note appended to the contract bead first.
//
// # Import direction
//
// This package imports stdlib ONLY — never internal/transport,
// internal/verbs, or internal/cli. That keeps the dependency graph acyclic:
// internal/transport imports sentlog to build OutboundMessage.Sender and the
// logging decorator; internal/verbs imports sentlog for the `ateam sent`
// verb. Either of those importing back into this package would cycle.
package sentlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kind identifies who/what sent one outbound message. Exactly six real
// values plus the UNDECLARED guard value below — this is the complete,
// frozen set (contract §3), verified by grepping every OutboundMessage
// struct literal in non-test Go (transport.OutboundMessage, brace opened
// on the following line — not written as one string here so this doc
// comment itself doesn't false-positive that grep): six literals, six
// senders, no others.
type Kind string

const (
	KindNotify         Kind = "notify"          // `ateam notify <initiative-id>`
	KindNotifyBriefing Kind = "notify-briefing" // `ateam notify briefing`
	KindNotifyDirect   Kind = "notify-direct"   // `ateam notify direct`
	KindDispatch       Kind = "dispatch"        // eager topic creation
	KindClose          Kind = "close"           // farewell on close
	KindRelayHung      Kind = "relay-hung"      // automatic hung/stall alert

	// KindUndeclared is never set by a call site. It is the decorator's own
	// guard value for an empty or unrecognised Sender (contract §1): the
	// record is still written, never dropped, and an "UNDECLARED" entry in
	// the audit trail is itself the bug report.
	KindUndeclared Kind = "UNDECLARED"
)

// knownKinds is the complete set of six real sender kinds a call site may
// declare. KindUndeclared is deliberately excluded: no call site may set it
// directly, so it must never report as "known".
var knownKinds = map[Kind]bool{
	KindNotify:         true,
	KindNotifyBriefing: true,
	KindNotifyDirect:   true,
	KindDispatch:       true,
	KindClose:          true,
	KindRelayHung:      true,
}

// Known reports whether k is one of the six declared sender kinds (contract
// §3). False for "", KindUndeclared, and any unrecognised value.
func (k Kind) Known() bool { return knownKinds[k] }

// Outcome values a Record's Outcome field takes — "sent" | "failed" ONLY
// (contract §2).
const (
	OutcomeSent   = "sent"
	OutcomeFailed = "failed"
)

// Record is one line of the sent-message log. Field order and json tags are
// FROZEN (contract §2) — do not reorder, rename, or retype.
type Record struct {
	Timestamp  string `json:"ts"`         // RFC3339 UTC
	Sender     Kind   `json:"sender"`     // DECLARED (§3)
	Transport  string `json:"transport"`  // from Transport.Name()
	Initiative string `json:"initiative"` // OutboundMessage.InitiativeID, or reserved "briefing"/"direct"
	ThreadRef  string `json:"thread_ref"` // resolved per §2.1
	General    bool   `json:"general"`    // OutboundMessage.General
	Title      string `json:"title"`      // OutboundMessage.Title
	Body       string `json:"body"`       // OutboundMessage.Body, FULL, never truncated
	Outcome    string `json:"outcome"`    // "sent" | "failed" ONLY
	Error      string `json:"error"`      // "" on success; on failure, RedactError(sendErr) — embedded URLs reduced to scheme://host (§6, amended)

	SessionID  string `json:"session_id"`  // DERIVED (§4). May be "". MAY BE STALE.
	Cwd        string `json:"cwd"`         // DERIVED
	StewardCwd bool   `json:"steward_cwd"` // DERIVED
	PID        int    `json:"pid"`         // DERIVED, os.Getpid()
}

// MarshalLine renders r as a single JSONL line: a JSON object followed by
// one "\n". Callers append the returned bytes with a single Write — see
// Append and contract §2.3 (one write, one line — no bufio.Writer, no
// second write for the newline).
func (r Record) MarshalLine() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("sentlog: marshal record: %w", err)
	}
	return append(data, '\n'), nil
}

// Path returns the sent-message log path: <home>/sent.jsonl (contract §5).
//
// Not under steward/: the log is machine-scoped, not steward-scoped — four
// of the six senders are not the Steward. Not synced: deliberately
// per-machine local state, like StewardFallbackMarkerPath — "who sent what
// from this machine" is per-machine forensics that buys nothing from
// dolt-backed sync while costing merge and lock traffic.
func Path(home string) string {
	return filepath.Join(home, "sent.jsonl")
}

// StewardMarkerPath returns the path to the Steward's own-session marker
// file: <home>/steward/session/.steward-session (contract §5). This is the
// single owning definition of that path — internal/verbs.
// StewardSessionMarkerPath delegates here so there is exactly one
// construction of it, rather than two independent joins drifting apart.
func StewardMarkerPath(home string) string {
	return filepath.Join(home, "steward", "session", ".steward-session")
}

// Append appends one Record as a single JSONL line to Path(home), creating
// the parent directory if needed. The append is ONE f.Write of the
// marshalled line on an O_APPEND fd: POSIX O_APPEND makes the seek-and-write
// atomic, so concurrent appenders from different processes never interleave
// provided each record is a single write() call (contract §2.3) — this is
// non-negotiable; do not introduce a bufio.Writer or a second Write.
func Append(home string, r Record) error {
	path := Path(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sentlog: create log dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("sentlog: open log: %w", err)
	}
	defer f.Close()

	line, err := r.MarshalLine()
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("sentlog: append: %w", err)
	}
	return nil
}

// sessionIDEnvVar is the env var Claude Code exports into every session's
// env carrying that session's id. NEVER "CLAUDE_SESSION_ID" — that spelling
// is wrong (internal/verbs/reap_orphans.go:60 has it; internal/verbs/
// messaging.go documents this spelling as verified-live).
const sessionIDEnvVar = "CLAUDE_CODE_SESSION_ID"

// unknownSessionSentinel is the literal value session-start-inbox.sh (and
// sibling hook scripts) fall back to when stdin carries no .session_id.
// Treated exactly like an absent session id (contract §4; matches
// internal/verbs/tie_session.go:68's convention) — never written to the log
// as a garbage "session_id":"unknown" line.
const unknownSessionSentinel = "unknown"

// Derive gathers the four derived-identity fields (contract §4) at send
// time: session_id, cwd, steward_cwd, pid. Every derivation error degrades
// to the zero value — Derive never fails, never panics, and must never be
// allowed to change the outcome of a send.
//
// NO RECONCILIATION with the caller's declared Sender happens here or
// anywhere else: both are stored as-is, even when they disagree. Making
// that discrepancy visible is the forensic property the whole log exists
// for.
func Derive(home string) Record {
	var rec Record

	if sid := os.Getenv(sessionIDEnvVar); sid != unknownSessionSentinel {
		rec.SessionID = sid
	}

	if cwd, err := os.Getwd(); err == nil {
		rec.Cwd = cwd
		rec.StewardCwd = isStewardCwd(home, cwd)
	}

	rec.PID = os.Getpid()

	return rec
}

// isStewardCwd reports whether cwd is the Steward's own session directory
// (or a subdirectory of it), identified by StewardMarkerPath(home) existing
// on disk. Must match internal/verbs.isStewardSession (messaging.go:479-487)
// exactly, including canonicalPath symlink normalisation on both sides
// (contract §4).
func isStewardCwd(home, cwd string) bool {
	marker := StewardMarkerPath(home)
	if _, err := os.Stat(marker); err != nil {
		return false
	}
	sessionDir := canonicalPath(filepath.Dir(marker))
	wantCwd := canonicalPath(cwd)
	return wantCwd == sessionDir || strings.HasPrefix(wantCwd, sessionDir+string(filepath.Separator))
}

// canonicalPath duplicates internal/verbs/match.go's helper of the same
// name. sentlog cannot import internal/verbs (see the package doc's IMPORT
// DIRECTION — verbs imports sentlog, never the reverse), so this 4-line
// symlink-normalisation helper is intentionally forked rather than shared.
// Must stay behaviorally identical to match.go's canonicalPath.
func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}
