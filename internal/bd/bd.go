// Package bd wraps the bd CLI for calls against a specific workspace home.
package bd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ExecFunc is the signature of the function used to run an external command.
// Swap it in tests via NewClientWithExec.
type ExecFunc func(name string, args ...string) (stdout []byte, stderr []byte, err error)

// defaultExec runs the named binary and returns its combined output split by
// stream. A non-zero exit is returned as err (wraps *exec.ExitError).
func defaultExec(name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(name, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// Client wraps bd for a specific workspace home.
type Client struct {
	home string
	exec ExecFunc
}

// NewClient returns a Client bound to home using the real bd binary.
func NewClient(home string) *Client {
	return &Client{home: home, exec: defaultExec}
}

// NewClientWithExec returns a Client that uses execFn instead of os/exec.
// Use this in tests to inject a fake runner.
func NewClientWithExec(home string, execFn ExecFunc) *Client {
	return &Client{home: home, exec: execFn}
}

// Run executes bd -C <home> [args...] and returns trimmed stdout. Any non-zero
// exit or exec error is returned as a non-nil error; stderr is appended to the
// error message for context.
func (c *Client) Run(args ...string) (string, error) {
	full := append([]string{"-C", c.home}, args...)
	out, errOut, err := c.exec("bd", full...)
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

// Issue represents the fields returned by `bd list --json` and `bd create --json`.
type Issue struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	Labels      []string `json:"labels"`
	Notes       string   `json:"notes"`
}
