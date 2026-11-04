package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// dispatcherRun reports whether this process runs inside a Dispatcher run:
// DSP_BEAD is non-empty. bead and sup are the DSP_BEAD and DSP_SUP values.
func dispatcherRun() (bead, sup string, ok bool) {
	bead = os.Getenv("DSP_BEAD")
	return bead, os.Getenv("DSP_SUP"), bead != ""
}

// outboxPath returns the path of the run's outbox file, $DSP_SUP/outbox.jsonl.
// It returns "" and no error outside a Dispatcher run, and an error inside a
// run whose DSP_SUP is empty.
func outboxPath() (string, error) {
	_, sup, ok := dispatcherRun()
	if !ok {
		return "", nil
	}
	if sup == "" {
		return "", errors.New("DSP_BEAD is set but DSP_SUP is empty")
	}
	return filepath.Join(sup, "outbox.jsonl"), nil
}

// outboxLabelOp is the outbox line that asks the Dispatcher to change bead
// labels: {"op":"label","add":[...],"remove":[...]}.
type outboxLabelOp struct {
	Op     string   `json:"op"`
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// MarshalJSON emits nil Add and Remove as [] instead of null.
func (o outboxLabelOp) MarshalJSON() ([]byte, error) {
	type plain outboxLabelOp
	if o.Add == nil {
		o.Add = []string{}
	}
	if o.Remove == nil {
		o.Remove = []string{}
	}
	return json.Marshal(plain(o))
}

// maxGateOutboxAskRunes caps the ask text in a gate outbox mail body.
const maxGateOutboxAskRunes = 8000

// outboxMailOp is the outbox line that asks the Dispatcher to file a mail
// bead: {"op":"file","type":"mail","title":...,"body":...}. The field order
// is part of the Dispatcher contract.
type outboxMailOp struct {
	Op    string `json:"op"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// newGateMailOp builds the mail op for a gate on initiative id. The body
// carries no agent: header. It truncates ask to maxGateOutboxAskRunes runes.
func newGateMailOp(id, kind, ask string) outboxMailOp {
	if r := []rune(ask); len(r) > maxGateOutboxAskRunes {
		ask = string(r[:maxGateOutboxAskRunes])
	}
	return outboxMailOp{
		Op:    "file",
		Type:  "mail",
		Title: id + ": " + kind + " gate",
		Body:  "initiative: " + id + "\nkind: " + kind + "\n---\n" + ask,
	}
}

// outboxHelperExitNothingWritten is the exit code of $DSP_SUP/bin/outbox-append
// when it wrote nothing: invalid JSON, a lock timeout, an fs error, or node
// missing. Its stderr names the cause.
const outboxHelperExitNothingWritten = 2

// outboxHelper returns the path of $DSP_SUP/bin/outbox-append when it is a
// regular executable file, and "" otherwise.
func outboxHelper(sup string) string {
	p := filepath.Join(sup, "bin", "outbox-append")
	if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
		return p
	}
	return ""
}

// appendOutboxOp appends op as one JSON line to the run's outbox. It does
// nothing outside a Dispatcher run. A marshal error writes nothing.
//
// When $DSP_SUP/bin/outbox-append is executable, it receives the line on stdin
// and appends and publishes it; ateam doesn't touch the outbox file. Exit code
// 2 (nothing written) is an error. Any other exit code counts as success,
// because the helper has already appended the line. The helper's stdout and stderr go
// to ateam's stderr, so ateam's own stdout stays parseable. Without the helper
// (an older Dispatcher or a local run), the line goes out in a single direct
// append.
func appendOutboxOp(op any) error {
	path, err := outboxPath()
	if err != nil || path == "" {
		return err
	}
	line, err := json.Marshal(op)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if helper := outboxHelper(filepath.Dir(path)); helper != "" {
		return runOutboxHelper(helper, line)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// runOutboxHelper pipes line to the outbox-append helper at path.
func runOutboxHelper(path string, line []byte) error {
	var stderr bytes.Buffer
	cmd := exec.Command(path)
	cmd.Stdin = bytes.NewReader(line)
	cmd.Stdout = os.Stderr
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exitErr):
		if exitErr.ExitCode() == outboxHelperExitNothingWritten {
			return fmt.Errorf("outbox-append wrote nothing: %s",strings.TrimSpace(stderr.String()))
		}
		return nil
	default:
		return fmt.Errorf("run outbox-append: %w", err)
	}
}
