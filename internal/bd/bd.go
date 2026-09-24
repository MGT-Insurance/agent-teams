// Package bd wraps the bd CLI for calls against a specific workspace home.
package bd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/procx"
)

// ExecFunc is the signature of the function used to run an external command
// with no ctx/deadline. Swap it in tests via NewClientWithExec. Prefer
// ContextExecFunc (RunContext) for any new caller that wants a bound.
type ExecFunc func(name string, args ...string) (stdout []byte, stderr []byte, err error)

// ContextExecFunc is the signature of the function used to run an external
// command bounded by ctx. Swap it in tests via NewClientWithContextExec.
type ContextExecFunc func(ctx context.Context, name string, args ...string) (stdout []byte, stderr []byte, err error)

// Client wraps bd for a specific workspace home.
type Client struct {
	home    string
	ctxExec ContextExecFunc
}

// NewClient returns a Client bound to home using the real bd binary. Calls
// made through RunContext are bounded by procx.RunBounded: if ctx expires
// before bd exits, the whole bd process group is killed.
func NewClient(home string) *Client {
	return &Client{home: home, ctxExec: procx.RunBounded}
}

// NewClientWithExec returns a Client that uses execFn instead of os/exec, for
// tests that don't care about ctx. execFn is adapted into a ContextExecFunc
// that ignores ctx, so Run (which calls RunContext with
// context.Background()) behaves exactly as before — no existing test needs
// to change.
func NewClientWithExec(home string, execFn ExecFunc) *Client {
	return &Client{home: home, ctxExec: func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
		return execFn(name, args...)
	}}
}

// NewClientWithContextExec returns a Client that uses fn for every exec, for
// tests that need to observe or fake ctx cancellation/timeout behavior.
func NewClientWithContextExec(home string, fn ContextExecFunc) *Client {
	return &Client{home: home, ctxExec: fn}
}

// Run executes bd -C <home> [args...] with no deadline and returns trimmed
// stdout. Equivalent to RunContext(context.Background(), args...).
func (c *Client) Run(args ...string) (string, error) {
	return c.RunContext(context.Background(), args...)
}

// RunContext executes bd -C <home> [args...] bounded by ctx and returns
// trimmed stdout. Any non-zero exit or exec error is returned as a non-nil
// error; stderr is appended to the error message for context.
//
// If ctx expires before bd exits, the whole bd process group is killed (see
// internal/procx) and the returned error wraps ctx.Err(), so
// errors.Is(err, context.DeadlineExceeded) holds for a caller that set a
// deadline.
func (c *Client) RunContext(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", c.home}, args...)
	out, errOut, err := c.ctxExec(ctx, "bd", full...)
	if err != nil {
		if len(errOut) > 0 {
			return "", fmt.Errorf("bd %s: %w\n%s", strings.Join(args, " "), err, strings.TrimRight(string(errOut), "\n"))
		}
		return "", fmt.Errorf("bd %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// RunJSON executes bd -C <home> [args...] and unmarshals the JSON stdout into
// dst. Designed for use with `--json` flags:
//
//	var issues []bd.Issue
//	err := client.RunJSON(&issues, "list", "--status=open", "--json")
func (c *Client) RunJSON(dst any, args ...string) error {
	out, err := c.Run(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(out), dst); err != nil {
		return fmt.Errorf("bd RunJSON: unmarshal: %w (raw: %.200s)", err, out)
	}
	return nil
}

// runner is a minimal interface for executing bd subcommands and returning
// stdout as a string. Used by ShowIssue to avoid importing the cli package.
type runner interface {
	Run(args ...string) (string, error)
}

// ShowIssue runs `bd show <id> --json`, which returns a single-element array,
// and returns the issue. The array shape is a quirk of the bd CLI; this helper
// unwraps it so callers work with a plain Issue.
func ShowIssue(r runner, id string) (Issue, error) {
	out, err := r.Run("show", id, "--json")
	if err != nil {
		return Issue{}, err
	}
	var issues []Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return Issue{}, fmt.Errorf("bd show %s: unmarshal: %w (raw: %.200s)", id, err, out)
	}
	if len(issues) == 0 {
		return Issue{}, fmt.Errorf("bd show %s: not found", id)
	}
	return issues[0], nil
}

// Issue represents the fields returned by `bd list --json` and `bd create --json`.
type Issue struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	ClosedAt    string   `json:"closed_at"`
	Labels      []string `json:"labels"`
	Notes       string   `json:"notes"`
	Assignee    string   `json:"assignee"`
	IssueType   string   `json:"issue_type"`
	CreatedBy   string   `json:"created_by"`
}
