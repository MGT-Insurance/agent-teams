package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDspEnvDispatcherRun(t *testing.T) {
	t.Setenv("DSP_SUP", "/sup")
	for _, c := range []struct {
		name  string
		set   bool
		value string
		ok    bool
	}{
		{"unset", false, "", false},
		{"empty", true, "", false},
		{"set", true, "bd-1", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.set {
				t.Setenv("DSP_BEAD", c.value)
			} else {
				t.Setenv("DSP_BEAD", "")
				os.Unsetenv("DSP_BEAD")
			}
			bead, sup, ok := dispatcherRun()
			if ok != c.ok || bead != c.value || sup != "/sup" {
				t.Errorf("dispatcherRun() = (%q, %q, %v), want (%q, %q, %v)", bead, sup, ok, c.value, "/sup", c.ok)
			}
		})
	}
}

func TestDspEnvOutboxPath(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	t.Setenv("DSP_SUP", "")
	if p, err := outboxPath(); p != "" || err != nil {
		t.Errorf("not in run: got (%q, %v), want (\"\", nil)", p, err)
	}

	t.Setenv("DSP_BEAD", "bd-1")
	if _, err := outboxPath(); err == nil || !strings.Contains(err.Error(), "DSP_BEAD is set but DSP_SUP is empty") {
		t.Errorf("in run without DSP_SUP: got err %v", err)
	}

	t.Setenv("DSP_SUP", "/sup")
	if p, err := outboxPath(); p != filepath.Join("/sup", "outbox.jsonl") || err != nil {
		t.Errorf("in run: got (%q, %v)", p, err)
	}
}

func TestDspEnvOutboxAppendOp(t *testing.T) {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "bd-1")
	t.Setenv("DSP_SUP", sup)

	if err := appendOutboxOp(outboxLabelOp{Op: "label", Add: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := appendOutboxOp(outboxLabelOp{Op: "label", Add: []string{"c"}, Remove: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(sup, "outbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("outbox does not end in a newline: %q", data)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), data)
	}
	if want := `{"op":"label","add":["a","b"],"remove":[]}`; lines[0] != want {
		t.Errorf("line 0 = %s, want %s", lines[0], want)
	}
	var op outboxLabelOp
	if err := json.Unmarshal([]byte(lines[1]), &op); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(op, outboxLabelOp{Op: "label", Add: []string{"c"}, Remove: []string{"a"}}) {
		t.Errorf("line 1 round trip = %+v", op)
	}
}

func TestDspEnvOutboxAppendOpNoopOutsideRun(t *testing.T) {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "")
	t.Setenv("DSP_SUP", sup)
	if err := appendOutboxOp(outboxLabelOp{Op: "label"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sup, "outbox.jsonl")); !os.IsNotExist(err) {
		t.Errorf("outbox file exists outside a run: %v", err)
	}
}

func TestDspEnvOutboxAppendOpMarshalErrorWritesNothing(t *testing.T) {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "bd-1")
	t.Setenv("DSP_SUP", sup)
	if err := appendOutboxOp(make(chan int)); err == nil {
		t.Fatal("want a marshal error")
	}
	if _, err := os.Stat(filepath.Join(sup, "outbox.jsonl")); !os.IsNotExist(err) {
		t.Errorf("outbox file exists after a marshal error: %v", err)
	}
}

func TestDriSessionArgsMatchesBgSessionArgs(t *testing.T) {
	bg := bgSessionArgs("n", "/dri at-1", "", "adv", "dri", "at-1", "")
	dri := driSessionArgs("/dri at-1", driSystemPromptAppend, "", "adv", "dri", "at-1", "")
	if !reflect.DeepEqual(bg[3:], dri) {
		t.Errorf("bgSessionArgs[3:] = %q\ndriSessionArgs = %q", bg[3:], dri)
	}
	if !reflect.DeepEqual(bg[:3], []string{"--bg", "-n", "n"}) {
		t.Errorf("bgSessionArgs prefix = %q", bg[:3])
	}
}

func TestDriSessionArgsExplicitAppendOnNonDriPrompt(t *testing.T) {
	args := driSessionArgs("wake: resume at-1", driSystemPromptAppend, "", "", "dri", "at-1", "")
	for i, a := range args {
		if a == "--append-system-prompt" {
			if args[i+1] != driSystemPromptAppend {
				t.Errorf("append value = %q, want driSystemPromptAppend", args[i+1])
			}
			return
		}
	}
	t.Errorf("no --append-system-prompt in %q", args)
}
