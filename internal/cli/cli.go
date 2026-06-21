// Package cli defines the Command interface, execution Context, typed exit
// errors, and verb Registry used by the ateam binary.
package cli

import (
	"fmt"
	"io"
)

// Context carries per-invocation state passed to every Command.Run call.
type Context struct {
	Home   string    // resolved workspace home (AGENT_TEAMS_HOME or $HOME/.agent-teams)
	BD     BDRunner  // bd client bound to Home
	Stdout io.Writer // defaults to os.Stdout
	Stderr io.Writer // defaults to os.Stderr
}

// BDRunner is the subset of bd.Client that Context exposes. Defined here as an
// interface so cli package stays import-cycle-free; bd.Client satisfies it.
type BDRunner interface {
	Run(args ...string) (string, error)
	RunJSON(dst any, args ...string) error
}

// Command is implemented by every ateam verb.
type Command interface {
	// Name returns the verb string (e.g. "list", "resume-match").
	Name() string
	// Run executes the verb. args contains everything after the verb token.
	// Return a typed error to control the exit code; nil -> exit 0.
	Run(ctx *Context, args []string) error
}

// Registry maps verb names to their Commands. Built once in main; never mutated
// after initial population.
type Registry map[string]Command

// Register adds cmd to r keyed by cmd.Name(). Panics on duplicate registration
// (programming error, not a runtime condition).
func (r Registry) Register(cmd Command) {
	if _, exists := r[cmd.Name()]; exists {
		panic(fmt.Sprintf("cli: duplicate verb registration: %q", cmd.Name()))
	}
	r[cmd.Name()] = cmd
}

// Lookup returns the Command for name and true, or nil and false.
func (r Registry) Lookup(name string) (Command, bool) {
	c, ok := r[name]
	return c, ok
}

// ExitCode maps an error returned by Command.Run to a process exit code.
//
//	nil               -> 0
//	*UsageError       -> 2
//	*DepError         -> 3
//	*WorkspaceError   -> 4
//	*SilentError      -> e.Code (caller already wrote output)
//	anything else     -> 1
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	switch e := err.(type) {
	case *UsageError:
		_ = e
		return 2
	case *DepError:
		_ = e
		return 3
	case *WorkspaceError:
		_ = e
		return 4
	case *SilentError:
		return e.Code
	default:
		return 1
	}
}

// UsageError signals a missing/unknown flag, missing positional, unknown verb,
// or empty verb. Exit code 2.
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string { return e.msg }

// Usagef constructs a *UsageError with a formatted message.
func Usagef(format string, a ...any) *UsageError {
	return &UsageError{msg: fmt.Sprintf(format, a...)}
}

// DepError signals that a required binary (bd, claude) is not in PATH.
// Exit code 3.
type DepError struct {
	msg string
}

func (e *DepError) Error() string { return e.msg }

// Depf constructs a *DepError with a formatted message.
func Depf(format string, a ...any) *DepError {
	return &DepError{msg: fmt.Sprintf(format, a...)}
}

// WorkspaceError signals that the workspace is not initialized (.beads missing).
// Exit code 4.
type WorkspaceError struct {
	msg string
}

func (e *WorkspaceError) Error() string { return e.msg }

// Workspacef constructs a *WorkspaceError with a formatted message.
func Workspacef(format string, a ...any) *WorkspaceError {
	return &WorkspaceError{msg: fmt.Sprintf(format, a...)}
}

// SilentError carries an explicit exit code. main does NOT print the error
// message — the verb has already written its own output to Stderr.
type SilentError struct {
	Code int
}

func (e *SilentError) Error() string { return fmt.Sprintf("exit %d", e.Code) }

// Silent constructs a *SilentError for the given exit code.
func Silent(code int) *SilentError {
	return &SilentError{Code: code}
}

// UsageText is printed to stderr for an empty or unknown verb.
const UsageText = "Usage: ateam <verb> [args]\n" +
	"Verbs: ws | list | list-json | human-list | audit | resume-match | resume-match-closed\n" +
	"       register | note | gate | clear-gate | learn | learnings\n" +
	"       show | close | reopen | sync | new-initiative | dispatch | resume | cost\n" +
	"       worktree-setup | send | inbox\n"
