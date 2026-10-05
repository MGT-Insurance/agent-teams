package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// Payload shapes come from the Codex 0.154.0 spike (hooks.log): a main-thread
// PreToolUse carries no agent_id; a child's carries agent_id + agent_type and
// shares the parent's session_id.
const (
	gateSID   = "01a10cb0-0dce-70f2-bf41-d7b4edded722"
	gateChild = "01a10cb2-6a4e-7481-81fc-d37968df6196"
)

func gatePre(sid, agentID, agentType, tool, command string) string {
	m := map[string]any{
		"session_id":      sid,
		"turn_id":         "01a10cb2-6ac1-7aa3-aa8d-a6d6a401670c",
		"cwd":             "/w",
		"hook_event_name": "PreToolUse",
		"model":           "gpt-5.6-luna",
		"permission_mode": "bypassPermissions",
		"tool_name":       tool,
		"tool_input":      map[string]any{"command": command},
		"tool_use_id":     "exec-f39a37cc-ad7c-4d78-9d01-d58752e52c2c",
	}
	if agentID != "" {
		m["agent_id"] = agentID
		m["agent_type"] = agentType
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func gateEvent(sid, agentID, agentType, event string) string {
	m := map[string]any{"session_id": sid, "cwd": "/w", "hook_event_name": event}
	if agentID != "" {
		m["agent_id"] = agentID
		m["agent_type"] = agentType
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func runGate(t *testing.T, home, event, payload string) string {
	t.Helper()
	ctx, stdout, _ := makeCtx(&fakeBD{}, home)
	if err := runCodexHook(ctx, event, strings.NewReader(payload), codexHookDeps{}); err != nil {
		t.Fatalf("%s: %v", event, err)
	}
	return stdout.String()
}

func wantDeny(t *testing.T, out, role string) {
	t.Helper()
	var o codexHookOutput
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	h := o.HookSpecificOutput
	if h == nil || h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" {
		t.Fatalf("want deny, got %q", out)
	}
	if h.PermissionDecisionReason != codexGateReason(role) {
		t.Fatalf("reason = %q", h.PermissionDecisionReason)
	}
	if !strings.HasSuffix(h.PermissionDecisionReason, "ateam learnings "+role) {
		t.Fatalf("reason must end with the bare command: %q", h.PermissionDecisionReason)
	}
}

func armMain(t *testing.T, home string) {
	t.Helper()
	codexGateArmMain(home, gateSID, "dri")
	if _, err := os.Stat(filepath.Join(home, "learnings-gate", gateSID, "main")); err != nil {
		t.Fatal(err)
	}
}

func TestCodexGateMainDeniesThenExactCommandClears(t *testing.T) {
	home := t.TempDir()
	armMain(t, home)
	wantDeny(t, runGate(t, home, "pre-tool-use", gatePre(gateSID, "", "", "Bash", "echo hi")), "dri")
	wantDeny(t, runGate(t, home, "pre-tool-use", gatePre(gateSID, "", "", "spawn_agent", "")), "dri")
	for _, cmd := range []string{"ateam learnings dri | head", "ateam learnings planner", "/x/bin/ateam learnings dri", "ateam learnings dri 2>&1"} {
		wantDeny(t, runGate(t, home, "pre-tool-use", gatePre(gateSID, "", "", "Bash", cmd)), "dri")
	}
	if out := runGate(t, home, "pre-tool-use", gatePre(gateSID, "", "", "Bash", "  ateam learnings dri \n")); out != "" {
		t.Fatalf("exact command output = %q, want empty", out)
	}
	if out := runGate(t, home, "pre-tool-use", gatePre(gateSID, "", "", "Bash", "echo hi")); out != "" {
		t.Fatalf("after clear output = %q, want empty", out)
	}
}

func TestCodexGateChildDeniedThenAllowedThenRearmedByPostCompact(t *testing.T) {
	home := t.TempDir()
	armMain(t, home)
	pre := func(cmd string) string {
		return runGate(t, home, "pre-tool-use", gatePre(gateSID, gateChild, "agent-teams-planner", "Bash", cmd))
	}
	wantDeny(t, pre("echo child-hi"), "planner")
	wantDeny(t, pre("ateam learnings dri"), "planner")
	if out := pre("ateam learnings planner"); out != "" {
		t.Fatalf("exact command output = %q", out)
	}
	if out := pre("echo child-hi"); out != "" {
		t.Fatalf("after load output = %q", out)
	}
	// A child's compaction re-arms that child only.
	runGate(t, home, "post-compact", gateEvent(gateSID, gateChild, "agent-teams-planner", "PostCompact"))
	wantDeny(t, pre("echo child-hi"), "planner")
	// A main-thread post-compact (no agent_id) touches nothing.
	runGate(t, home, "post-compact", gateEvent(gateSID, "", "", "PostCompact"))
	wantDeny(t, pre("echo child-hi"), "planner")
}

func TestCodexGateChildIndependentOfMainMarker(t *testing.T) {
	home := t.TempDir()
	// No main marker; a child dir exists. Child is still armed.
	if err := os.MkdirAll(filepath.Join(home, "learnings-gate", gateSID), 0o755); err != nil {
		t.Fatal(err)
	}
	wantDeny(t, runGate(t, home, "pre-tool-use", gatePre(gateSID, gateChild, "agent-teams-tester", "Bash", "ls")), "tester")
}

func TestCodexGateFailsOpen(t *testing.T) {
	home := t.TempDir()
	armMain(t, home)
	for name, payload := range map[string]string{
		"bad json":           `{not json`,
		"empty":              ``,
		"no session":         gatePre("", "", "", "Bash", "ls"),
		"traversal session":  gatePre("../x", "", "", "Bash", "ls"),
		"long session":       gatePre(strings.Repeat("a", 129), "", "", "Bash", "ls"),
		"unknown agent type": gatePre(gateSID, gateChild, "spike-role", "Bash", "ls"),
		"unknown role":       gatePre(gateSID, gateChild, "agent-teams-nosuchrole", "Bash", "ls"),
		"invalid agent id":   gatePre(gateSID, "../x", "agent-teams-planner", "Bash", "ls"),
		"other session":      gatePre("other-session", "", "", "Bash", "ls"),
	} {
		t.Run(name, func(t *testing.T) {
			if out := runGate(t, home, "pre-tool-use", payload); out != "" {
				t.Fatalf("output = %q, want empty", out)
			}
		})
	}
	t.Run("no gate dir", func(t *testing.T) {
		if out := runGate(t, t.TempDir(), "pre-tool-use", gatePre(gateSID, "", "", "Bash", "ls")); out != "" {
			t.Fatalf("output = %q", out)
		}
	})
	t.Run("unknown main role", func(t *testing.T) {
		h := t.TempDir()
		codexGateArmMain(h, gateSID, "bogus")
		root := filepath.Join(h, "learnings-gate", gateSID)
		if err := os.WriteFile(filepath.Join(root, "main"), []byte("bogus\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out := runGate(t, h, "pre-tool-use", gatePre(gateSID, "", "", "Bash", "ls")); out != "" {
			t.Fatalf("output = %q", out)
		}
	})
	t.Run("tool_input not an object", func(t *testing.T) {
		wantDeny(t, runGate(t, home, "pre-tool-use", `{"session_id":"`+gateSID+`","tool_name":"wait_agent","tool_input":"x"}`), "dri")
	})
}

func TestCodexGateSessionEndRemovesRoot(t *testing.T) {
	home := t.TempDir()
	armMain(t, home)
	if out := runGate(t, home, "session-end", gateEvent(gateSID, "", "", "SessionEnd")); out != "" {
		t.Fatalf("output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(home, "learnings-gate", gateSID)); !os.IsNotExist(err) {
		t.Fatalf("gate root still present: %v", err)
	}
	// Idempotent and fail open.
	runGate(t, home, "session-end", gateEvent(gateSID, "", "", "SessionEnd"))
	runGate(t, home, "session-end", `{bad`)
}

func TestCodexGateSessionStartArmsMainOnlyWhenInitiativeResolves(t *testing.T) {
	for _, source := range []string{"compact", "clear", "startup", "resume"} {
		for _, resolves := range []bool{true, false} {
			t.Run(source, func(t *testing.T) {
				home := t.TempDir()
				ctx, _, _ := makeCtx(&fakeBD{}, home)
				deps := codexHookDeps{
					resolve: func(*cli.Context, string, string) (bd.Issue, error) {
						if !resolves {
							return bd.Issue{}, errCodexHookNoInitiative
						}
						return bd.Issue{ID: "at-codex"}, nil
					},
					tie:    func(*cli.Context, string, string) error { return nil },
					unread: func(*cli.Context, string) ([]bd.Issue, error) { return nil, nil },
				}
				payload := `{"session_id":"` + gateSID + `","cwd":"/w","hook_event_name":"SessionStart","source":"` + source + `"}`
				if err := runCodexHook(ctx, "session-start", strings.NewReader(payload), deps); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(home, "learnings-gate", gateSID, "main"))
				want := resolves && (source == "compact" || source == "clear")
				if want {
					if err != nil || strings.TrimSpace(string(data)) != "dri" {
						t.Fatalf("main marker = %q, %v; want dri", data, err)
					}
				} else if err == nil {
					t.Fatalf("main marker present (%q), want none", data)
				}
			})
		}
	}
}

// TestCodexGateDenyTextMatchesShell pins the Go deny reason to the Claude
// shell text: it begins with the shell reason's leading sentences and both
// end with the identical command sentence, with the Codex sentence between.
func TestCodexGateDenyTextMatchesShell(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "plugins", "agent-teams", "hooks", "scripts", "lib", "learnings-gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)lg_deny_reason\(\) \{.*?printf '([^']*)'`).FindSubmatch(src)
	if m == nil {
		t.Fatal("lg_deny_reason printf not found")
	}
	shell := strings.ReplaceAll(string(m[1]), "%s", "planner")
	const marker = "Run this Bash command"
	i := strings.Index(shell, marker)
	if i < 0 {
		t.Fatalf("shell reason lacks %q: %q", marker, shell)
	}
	lead, tail := shell[:i], shell[i:]
	got := codexGateReason("planner")
	if !strings.HasPrefix(got, lead) {
		t.Errorf("Go reason does not begin with shell lead:\n go: %q\nsh: %q", got, lead)
	}
	if !strings.HasSuffix(got, tail) {
		t.Errorf("Go reason does not end with shell tail:\n go: %q\nsh: %q", got, tail)
	}
	if !strings.Contains(got, "In Codex, run it as its own exec_command call with max_output_tokens set to 10000 so the output is not truncated.") {
		t.Errorf("Go reason lacks the Codex sentence: %q", got)
	}
}
