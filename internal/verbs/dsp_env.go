package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

// appendOutboxOp appends op as one JSON line to the run's outbox file. It does
// nothing outside a Dispatcher run. The line goes out in a single write, and a
// marshal error writes nothing.
func appendOutboxOp(op any) error {
	path, err := outboxPath()
	if err != nil || path == "" {
		return err
	}
	line, err := json.Marshal(op)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
