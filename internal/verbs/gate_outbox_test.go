package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// outboxLines reads the run's outbox file and returns its non-empty lines.
func outboxLines(t *testing.T, sup string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sup, "outbox.jsonl"))
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func decodeMail(t *testing.T, line string) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("outbox line is not JSON: %v\n%s", err, line)
	}
	return m
}

func TestGateOutbox_StructuredInRun(t *testing.T) {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "dsp-1")
	t.Setenv("DSP_SUP", sup)
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}})
	g := &gateKong{ID: "at-9", Kind: "review", Decision: "Ship it?", Recommendation: "Yes", Alternative: "No"}
	if err := g.Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	lines := outboxLines(t, sup)
	if len(lines) != 1 {
		t.Fatalf("want 1 outbox line, got %d: %v", len(lines), lines)
	}
	m := decodeMail(t, lines[0])
	if m["op"] != "file" || m["type"] != "mail" {
		t.Errorf("op/type = %q/%q", m["op"], m["type"])
	}
	if m["title"] != "at-9: review gate" {
		t.Errorf("title = %q", m["title"])
	}
	want := "initiative: at-9\nkind: review\n---\nShip it?\n\nRecommended: Yes\nAlternative: No"
	if m["body"] != want {
		t.Errorf("body = %q, want %q", m["body"], want)
	}
	if !strings.HasPrefix(lines[0], `{"op":"file","type":"mail","title":"`) {
		t.Errorf("field order wrong: %s", lines[0])
	}
}

func TestGateOutbox_ProseAndKinds(t *testing.T) {
	for _, kind := range []string{"question", "review", "live-test-review"} {
		t.Run(kind, func(t *testing.T) {
			sup := t.TempDir()
			t.Setenv("DSP_BEAD", "dsp-1")
			t.Setenv("DSP_SUP", sup)
			f := makeTempFile(t, "need a call on X")
			ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}})
			if err := (&gateKong{ID: "at-3", File: f, Kind: kind}).Run(ctx); err != nil {
				t.Fatalf("gate: %v", err)
			}
			lines := outboxLines(t, sup)
			if len(lines) != 1 {
				t.Fatalf("want 1 line, got %d", len(lines))
			}
			m := decodeMail(t, lines[0])
			if m["title"] != "at-3: "+kind+" gate" {
				t.Errorf("title = %q", m["title"])
			}
			want := "initiative: at-3\nkind: " + kind + "\n---\nneed a call on X"
			if m["body"] != want {
				t.Errorf("body = %q, want %q", m["body"], want)
			}
			for _, l := range strings.Split(m["body"], "\n") {
				if strings.HasPrefix(l, "agent:") {
					t.Errorf("body has agent: header line %q", l)
				}
			}
		})
	}
}

func TestGateOutbox_Truncation(t *testing.T) {
	// A 3-byte rune straddles the 8000-rune boundary: rune 8000 is multibyte,
	// rune 8001 is cut.
	ask := strings.Repeat("a", 7999) + "€€€"
	op := newGateMailOp("at-1", "question", ask)
	got := strings.TrimPrefix(op.Body, "initiative: at-1\nkind: question\n---\n")
	if n := utf8.RuneCountInString(got); n != 8000 {
		t.Errorf("rune count = %d, want 8000", n)
	}
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "a€") {
		t.Errorf("truncation split a rune or cut wrong place: tail %q", got[len(got)-6:])
	}
	short := strings.Repeat("b", 8000)
	if g := strings.TrimPrefix(newGateMailOp("at-1", "question", short).Body, "initiative: at-1\nkind: question\n---\n"); g != short {
		t.Errorf("8000-rune ask must pass unchanged")
	}
}

func TestGateOutbox_NoRunNoFile(t *testing.T) {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "")
	os.Unsetenv("DSP_BEAD")
	t.Setenv("DSP_SUP", sup)
	f := makeTempFile(t, "q")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&gateKong{ID: "at-2", File: f, Kind: "question"}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sup, "outbox.jsonl")); !os.IsNotExist(err) {
		t.Errorf("outbox.jsonl must not exist outside a run (stat err %v)", err)
	}
}

func TestGateOutbox_FailureIsNonFatal(t *testing.T) {
	cases := map[string]string{
		"empty DSP_SUP":      "",
		"unwritable DSP_SUP": filepath.Join(t.TempDir(), "missing", "dir"),
	}
	for name, sup := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DSP_BEAD", "dsp-1")
			t.Setenv("DSP_SUP", sup)
			f := makeTempFile(t, "q")
			ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
			if err := (&gateKong{ID: "at-2", File: f, Kind: "question"}).Run(ctx); err != nil {
				t.Fatalf("gate must succeed despite outbox failure: %v", err)
			}
			if len(*calls) != 1 {
				t.Errorf("bd update calls = %d, want 1", len(*calls))
			}
			if !strings.Contains(ctx.Stderr.(interface{ String() string }).String(), "ateam gate: warning:") {
				t.Errorf("expected warning on stderr")
			}
		})
	}
}

func TestGateOutbox_AttachWarnsInRun(t *testing.T) {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "dsp-1")
	t.Setenv("DSP_SUP", sup)
	f := makeTempFile(t, "proof")
	shot := makeTempFile(t, "png")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&gateKong{ID: "at-5", File: f, Kind: "live-test-review", Attach: []string{shot}}).Run(ctx); err != nil {
		t.Fatalf("gate must still succeed: %v", err)
	}
	stderr := ctx.Stderr.(interface{ String() string }).String()
	if strings.Count(stderr, "--attach files are not delivered from a cloud run") != 1 || !strings.Contains(stderr, "screenshot: <bead id>") {
		t.Errorf("want exactly one attach warning, got %q", stderr)
	}
	if len(outboxLines(t, sup)) != 1 {
		t.Errorf("mail op must still be filed")
	}
}

func TestGateOutbox_NoAttachNoWarningInRun(t *testing.T) {
	t.Setenv("DSP_BEAD", "dsp-1")
	t.Setenv("DSP_SUP", t.TempDir())
	f := makeTempFile(t, "proof")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&gateKong{ID: "at-5", File: f, Kind: "live-test-review"}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if strings.Contains(ctx.Stderr.(interface{ String() string }).String(), "not delivered") {
		t.Errorf("no warning expected without --attach")
	}
}

func TestGateOutbox_AttachNoWarningLocally(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	os.Unsetenv("DSP_BEAD")
	f := makeTempFile(t, "proof")
	shot := makeTempFile(t, "png")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&gateKong{ID: "at-5", File: f, Kind: "live-test-review", Attach: []string{shot}}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if strings.Contains(ctx.Stderr.(interface{ String() string }).String(), "not delivered") {
		t.Errorf("no warning expected locally")
	}
}

// writeVerdict writes a verdict.md under its own temp dir and returns its path.
func writeVerdict(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "verdict.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write verdict: %v", err)
	}
	return p
}

func runGateInRun(t *testing.T, g *gateKong) (body, stderr string) {
	t.Helper()
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "dsp-1")
	t.Setenv("DSP_SUP", sup)
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := g.Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	lines := outboxLines(t, sup)
	if len(lines) != 1 {
		t.Fatalf("want 1 outbox line, got %d", len(lines))
	}
	return decodeMail(t, lines[0])["body"], ctx.Stderr.(interface{ String() string }).String()
}

func TestGateOutbox_VerdictNamesScreenshotInRun(t *testing.T) {
	v := writeVerdict(t, "bead: dsp-9\nverdict: pass\n")
	shot := makeTempFile(t, "png")
	body, stderr := runGateInRun(t, &gateKong{ID: "at-5", File: makeTempFile(t, "proof"), Kind: "live-test-review", Attach: []string{v, shot}})
	want := "initiative: at-5\nkind: live-test-review\n---\nscreenshot: dsp-9\nproof"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
	if strings.Contains(stderr, "not delivered") {
		t.Errorf("no warning expected, got %q", stderr)
	}
}

func TestGateOutbox_VerdictDedupesAgainstAsk(t *testing.T) {
	v := writeVerdict(t, "bead: dsp-9\n")
	body, _ := runGateInRun(t, &gateKong{ID: "at-5", File: makeTempFile(t, "see\nscreenshot: dsp-9\n"), Kind: "question", Attach: []string{v}})
	if n := strings.Count(body, "screenshot: dsp-9"); n != 1 {
		t.Errorf("screenshot line count = %d, want 1: %q", n, body)
	}
}

func TestGateOutbox_VerdictStructuredAndMultiple(t *testing.T) {
	v1 := writeVerdict(t, "bead: dsp-9\n")
	v2 := writeVerdict(t, "bead: dsp-10\n")
	v3 := writeVerdict(t, "bead: dsp-9\n")
	body, _ := runGateInRun(t, &gateKong{ID: "at-9", Kind: "review", Decision: "Ship it?", Recommendation: "Yes", Alternative: "No", Attach: []string{v1, v2, v3}})
	want := "initiative: at-9\nkind: review\n---\nscreenshot: dsp-9\nscreenshot: dsp-10\nShip it?\n\nRecommended: Yes\nAlternative: No"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestGateOutbox_VerdictWithoutValidBeadKeepsWarning(t *testing.T) {
	for name, content := range map[string]string{
		"no bead line": "verdict: pass\nbead: dsp-9\n",
		"invalid id":   "bead: -bad id\n",
		"empty id":     "bead:\n",
	} {
		t.Run(name, func(t *testing.T) {
			v := writeVerdict(t, content)
			body, stderr := runGateInRun(t, &gateKong{ID: "at-5", File: makeTempFile(t, "proof"), Kind: "question", Attach: []string{v}})
			if strings.Contains(body, "screenshot:") {
				t.Errorf("no screenshot line expected: %q", body)
			}
			if strings.Count(stderr, "--attach files are not delivered from a cloud run") != 1 {
				t.Errorf("want warning kept, got %q", stderr)
			}
		})
	}
}

func TestGateOutbox_VerdictLocalUnchanged(t *testing.T) {
	sup := t.TempDir()
	t.Setenv("DSP_BEAD", "")
	os.Unsetenv("DSP_BEAD")
	t.Setenv("DSP_SUP", sup)
	v := writeVerdict(t, "bead: dsp-9\n")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}})
	var got []Attachment
	g := &gateKong{ID: "at-5", File: makeTempFile(t, "proof"), Kind: "question", Attach: []string{v}}
	g.notify = func(_ *cli.Context, _ string, _ string, a []Attachment) error { got = a; return nil }
	if err := g.Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sup, "outbox.jsonl")); !os.IsNotExist(err) {
		t.Errorf("no outbox expected locally")
	}
	if len(got) != 1 || got[0].Path != v || got[0].Kind != "document" {
		t.Errorf("notify attachments = %+v", got)
	}
	if s := ctx.Stderr.(interface{ String() string }).String(); s != "" {
		t.Errorf("stderr = %q, want empty", s)
	}
}
