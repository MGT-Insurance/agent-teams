package verbs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeInstructionsFile writes content to
// <home>/instructions/<role>.md, creating the directory as needed.
func writeInstructionsFile(t *testing.T, home, role string, content []byte) string {
	t.Helper()
	dir := filepath.Join(home, "instructions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, role+".md")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// TestInstructions_PresentUnderCap covers path 1: a file under the cap is
// served with a header, the verbatim body, and a matching trailer. The body
// must be byte-identical to the file — no trimming, no re-wrapping.
func TestInstructions_PresentUnderCap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_HOME", home)

	body := "Always run `go vet` before committing.\nNever use `git push --force`.\n"
	path := writeInstructionsFile(t, home, "reviewer", []byte(body))

	ctx, stdout, _ := makeCtx(nil, home)
	cmd := &instructionsKong{Role: "reviewer"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	if !strings.Contains(out, body) {
		t.Errorf("expected verbatim body in output; got:\n%s", out)
	}
	if !strings.Contains(out, path) {
		t.Errorf("expected header to name the source path %q; got:\n%s", path, out)
	}
	if !strings.Contains(out, "HUMAN-AUTHORED") {
		t.Errorf("expected header to declare HUMAN-AUTHORED; got:\n%s", out)
	}
	if !strings.Contains(out, "machine-local") {
		t.Errorf("expected header to declare machine-local; got:\n%s", out)
	}
	if !strings.Contains(out, "never condensed") {
		t.Errorf("expected header to declare it is never condensed; got:\n%s", out)
	}
	if !strings.Contains(out, "EXTENDS") || strings.Contains(out, "override") == false {
		t.Errorf("expected header to state EXTENDS/does-not-override precedence; got:\n%s", out)
	}

	// Header and trailer must share the same stats line so a reader can
	// detect truncation by comparing the two ends.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	header := lines[0]
	trailer := lines[len(lines)-1]
	if !strings.HasPrefix(header, "[instructions reviewer") {
		t.Errorf("expected header to start with '[instructions reviewer'; got: %s", header)
	}
	if !strings.HasPrefix(trailer, "[instructions reviewer") {
		t.Errorf("expected trailer to start with '[instructions reviewer'; got: %s", trailer)
	}
	// Extract the "(N bytes)" fragment from each and require it to match.
	statOf := func(s string) string {
		i := strings.Index(s, "(")
		j := strings.Index(s, " bytes)")
		if i == -1 || j == -1 {
			t.Fatalf("could not find byte-count stat in line: %s", s)
		}
		return s[i : j+len(" bytes)")]
	}
	headerStat := statOf(header)
	trailerStat := statOf(trailer)
	if headerStat != trailerStat {
		t.Errorf("header stat %q does not match trailer stat %q", headerStat, trailerStat)
	}
	wantStat := "(" + strconv.Itoa(len(body)) + " bytes)"
	if headerStat != wantStat {
		t.Errorf("expected stat %q, got %q", wantStat, headerStat)
	}
}

// TestInstructions_Absent covers path 2: no file for the role is exactly
// "[instructions <role>: none]" and exit 0.
func TestInstructions_Absent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_HOME", home)

	ctx, stdout, _ := makeCtx(nil, home)
	cmd := &instructionsKong{Role: "planner"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "[instructions planner: none]\n"
	if stdout.String() != want {
		t.Errorf("got %q, want %q", stdout.String(), want)
	}
}

// TestInstructions_UnknownRoleIsAbsent confirms an unrecognized role is
// simply treated as absent — no validation against a role allowlist.
func TestInstructions_UnknownRoleIsAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_HOME", home)
	writeInstructionsFile(t, home, "reviewer", []byte("reviewer-only content"))

	ctx, stdout, _ := makeCtx(nil, home)
	cmd := &instructionsKong{Role: "not-a-real-role"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "[instructions not-a-real-role: none]\n"
	if stdout.String() != want {
		t.Errorf("got %q, want %q", stdout.String(), want)
	}
}

// TestInstructions_RoleSelectsFile confirms the role argument picks the file
// (instructions/reviewer.md vs instructions/planner.md), not the other way
// around.
func TestInstructions_RoleSelectsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_HOME", home)
	writeInstructionsFile(t, home, "reviewer", []byte("reviewer body"))
	writeInstructionsFile(t, home, "planner", []byte("planner body"))

	ctx, stdout, _ := makeCtx(nil, home)
	cmd := &instructionsKong{Role: "planner"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "planner body") {
		t.Errorf("expected planner body in output; got:\n%s", out)
	}
	if strings.Contains(out, "reviewer body") {
		t.Errorf("did not expect reviewer body in output; got:\n%s", out)
	}
}

// TestInstructions_AtCapBoundaryServed covers the cap boundary: a file of
// exactly instructionsCapBytes must be served, not refused — the boundary is
// inclusive (mirrors write_test.go's at-cap cases, e.g.
// TestLearn_ColdBetween900And1500).
func TestInstructions_AtCapBoundaryServed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_HOME", home)

	body := strings.Repeat("a", instructionsCapBytes)
	writeInstructionsFile(t, home, "reviewer", []byte(body))

	ctx, stdout, _ := makeCtx(nil, home)
	cmd := &instructionsKong{Role: "reviewer"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if strings.Contains(out, "OVER CAP") {
		t.Errorf("file at exactly the cap must be served, not refused; got:\n%s", out)
	}
	if !strings.Contains(out, body) {
		t.Errorf("expected the at-cap body to be served verbatim; got %d bytes of output", len(out))
	}
}

// TestInstructions_OneByteOverCapRefused covers path 3: a file one byte over
// the cap must emit NO body anywhere in stdout, plus a marker naming the
// path and both the actual size and the cap. Exit is still 0 (nil error).
func TestInstructions_OneByteOverCapRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_HOME", home)

	// A distinctive marker embedded in an otherwise-filler body: if this
	// string shows up anywhere in stdout, the cap was not enforced.
	const marker = "THIS-BODY-MUST-NEVER-BE-EMITTED-a1b2c3"
	filler := strings.Repeat("x", instructionsCapBytes+1-len(marker))
	body := marker + filler
	if len(body) != instructionsCapBytes+1 {
		t.Fatalf("test fixture wrong size: got %d, want %d", len(body), instructionsCapBytes+1)
	}
	path := writeInstructionsFile(t, home, "reviewer", []byte(body))

	ctx, stdout, _ := makeCtx(nil, home)
	cmd := &instructionsKong{Role: "reviewer"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if strings.Contains(out, marker) {
		t.Fatalf("over-cap body leaked into stdout (found marker fragment); got:\n%s", out)
	}
	if !strings.Contains(out, path) {
		t.Errorf("expected refusal marker to name the file path %q; got:\n%s", path, out)
	}
	if !strings.Contains(out, strconv.Itoa(len(body))) {
		t.Errorf("expected refusal marker to name the actual size %d; got:\n%s", len(body), out)
	}
	if !strings.Contains(out, strconv.Itoa(instructionsCapBytes)) {
		t.Errorf("expected refusal marker to name the cap %d; got:\n%s", instructionsCapBytes, out)
	}
}

// TestInstructions_MultibyteBodyRoundTrips confirms the cap is measured in
// bytes (not runes) and that multibyte content survives the round trip
// unmangled.
func TestInstructions_MultibyteBodyRoundTrips(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_TEAMS_HOME", home)

	body := "Prefer café-style commit messages: emoji 🎉 optional, but be précis.\n"
	writeInstructionsFile(t, home, "reviewer", []byte(body))

	ctx, stdout, _ := makeCtx(nil, home)
	cmd := &instructionsKong{Role: "reviewer"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, body) {
		t.Errorf("expected multibyte body to survive round-trip unmangled; got:\n%s", out)
	}
	// The stats line reports len(body) in bytes, which for this fixture is
	// larger than its rune count — assert byte-count is what's reported.
	byteLen := len(body)
	if byteLen == len([]rune(body)) {
		t.Fatalf("test fixture is not actually multibyte: byte len == rune len")
	}
	if !strings.Contains(out, strconv.Itoa(byteLen)+" bytes") {
		t.Errorf("expected stats to report byte length %d, not rune length; got:\n%s", byteLen, out)
	}
}
