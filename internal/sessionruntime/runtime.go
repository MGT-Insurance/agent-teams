// Package sessionruntime defines the runtime-scoped session identity and the
// narrow adapter seam used by dispatch, resume, and delivery coordination.
package sessionruntime

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Kind identifies the program that owns an agent session.
type Kind string

const (
	Claude Kind = "claude"
	Codex  Kind = "codex"
)

// SessionRef is an opaque session identifier scoped to its owning runtime.
type SessionRef struct {
	Runtime Kind
	ID      string
}

// Request is the runtime-neutral input for one runtime turn request.
type Request struct {
	InitiativeID string
	// AgentTeamsHome is the resolved global workspace for this initiative.
	// Managed Codex threads outlive the submitting ateam process, so this value
	// must be made sticky on the thread instead of relying on daemon process env.
	AgentTeamsHome string
	// AutoCompactWindow is the optional per-thread Codex compaction token limit
	// resolved by the runtime worker. Nil preserves Codex's native model default.
	AutoCompactWindow *int64
	Worktree          string
	Prompt            string
	Model             string
	Events            io.Writer
	Stderr            io.Writer
}

// SessionSink durably binds a newly observed session to its initiative.
type SessionSink func(SessionRef) error

// Adapter submits one runtime turn. Claude remains process-oriented outside
// this interface. Codex returns after app-server accepts the turn; its managed
// daemon owns the turn independently of the client connection.
type Adapter interface {
	Kind() Kind
	Launch(context.Context, Request, SessionSink) error
	Resume(context.Context, Request, SessionRef) error
}

// ParseKind validates a concrete runtime. "auto" is a selection instruction,
// not a durable kind, and is therefore rejected here.
func ParseKind(value string) (Kind, error) {
	switch Kind(strings.TrimSpace(value)) {
	case Claude:
		return Claude, nil
	case Codex:
		return Codex, nil
	default:
		return "", fmt.Errorf("unknown runtime %q (supported: claude, codex)", value)
	}
}

// DefaultResolver lazily reads a selected dispatch-class runtime default.
// The bool reports whether that class has a configured value.
type DefaultResolver func() (string, bool, error)

// ResolveNew chooses the concrete runtime for a newly dispatched initiative.
// The explicit value wins; empty and "auto" consult ATEAM_RUNTIME, the lazy
// selected config default, and then preserve the legacy Claude default.
func ResolveNew(explicit, machineDefault string, configDefault DefaultResolver) (Kind, error) {
	if explicit != "" && explicit != "auto" {
		return ParseKind(explicit)
	}
	if machineDefault != "" {
		return ParseKind(machineDefault)
	}
	if configDefault != nil {
		value, configured, err := configDefault()
		if err != nil {
			return "", err
		}
		if configured {
			return ParseKind(value)
		}
	}
	return Claude, nil
}

// ResolveStored resolves durable initiative metadata. Missing runtime is the
// legacy Claude representation; an unknown non-empty value fails closed.
func ResolveStored(stored string) (Kind, error) {
	if stored == "" {
		return Claude, nil
	}
	return ParseKind(stored)
}

// AssertStored checks an optional resume flag against durable metadata.
func AssertStored(stored, asserted string) (Kind, error) {
	kind, err := ResolveStored(stored)
	if err != nil {
		return "", err
	}
	if asserted == "" {
		return kind, nil
	}
	want, err := ParseKind(asserted)
	if err != nil {
		return "", err
	}
	if want != kind {
		return "", fmt.Errorf("runtime assertion %q does not match initiative runtime %q", want, kind)
	}
	return kind, nil
}

// EventLogPath is the reconstructible JSONL output for an initiative's Codex
// launches and resumes. Worker state and locks use sibling paths in Phase 2.
func EventLogPath(home, initiativeID string) string {
	return filepath.Join(home, "runtimes", string(Codex), initiativeID+".jsonl")
}
