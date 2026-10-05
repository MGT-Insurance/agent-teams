package verbs

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Codex parity for the Claude learnings gate (agent-teams-75h7.1 contract,
// items 1, 2, 4, 6, 7, 10; agent-teams-75h7.6). The on-disk layout matches the
// Claude hooks, minus the spawn registry: a Codex child's agent_type is always
// the role's TOML name.
//
//	<ATH>/learnings-gate/<session_id>/main            role of an armed main thread
//	<ATH>/learnings-gate/<session_id>/loaded/<id>     child <id> has loaded
//
// Every path here fails open: a bad payload, an invalid id, or an unreadable
// file prints nothing and allows the call.

const codexGateDirName = "learnings-gate"

var codexGateIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

const (
	codexGateReasonLead = "BLOCKED: your agent-teams %s learnings are not loaded in this context. Every other tool call stays blocked until you load them. Read the whole output when you do."
	// The Codex sentence sits before the command so the reason still ends
	// with the bare command: agents copy any punctuation that follows it.
	codexGateReasonCodex = " In Codex, run it as its own exec_command call with max_output_tokens set to 10000 so the output is not truncated."
	codexGateReasonTail  = " Run this Bash command by itself, exactly as written, with no period or anything else added: ateam learnings %s"
)

type codexGateInput struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

func codexGateReason(role string) string {
	return strings.ReplaceAll(codexGateReasonLead+codexGateReasonCodex+codexGateReasonTail, "%s", role)
}

// codexGateRoot returns the gate root for a session, or "" when the session
// id is invalid (no gate).
func codexGateRoot(home, sessionID string) string {
	if home == "" || !codexGateIDPattern.MatchString(sessionID) {
		return ""
	}
	return filepath.Join(home, codexGateDirName, sessionID)
}

// codexGateArmMain writes the main-thread marker. Errors are ignored: a gate
// that cannot arm simply does not gate.
func codexGateArmMain(home, sessionID, role string) {
	root := codexGateRoot(home, sessionID)
	if root == "" {
		return
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(root, "main"), []byte(role+"\n"), 0o644)
}

// codexGateChildRole returns the role for a Codex child agent_type, or ""
// when it is not an embedded agent-teams role.
func codexGateChildRole(agentType string) string {
	role, ok := strings.CutPrefix(agentType, "agent-teams-")
	if !ok || !codexGateIDPattern.MatchString(role) {
		return ""
	}
	if _, err := codexAgentDefinitions.ReadFile("codex_agents/agent-teams-" + role + ".toml"); err != nil {
		return ""
	}
	return role
}

// decodeCodexGateInput reports false when the payload has no usable session.
func decodeCodexGateInput(input io.Reader) (codexGateInput, bool) {
	var in codexGateInput
	// A tool_input that is not an object leaves the other fields usable, so
	// a decode error only matters when no session id was read.
	_ = json.NewDecoder(input).Decode(&in)
	return in, in.SessionID != ""
}

// codexGatePreToolUse denies every call except the exact learnings command
// while the main thread or the calling child is armed.
func codexGatePreToolUse(home string, stdout io.Writer, input io.Reader) {
	in, ok := decodeCodexGateInput(input)
	if !ok {
		return
	}
	root := codexGateRoot(home, in.SessionID)
	if root == "" {
		return
	}
	var role string
	var clear func()
	if in.AgentID == "" {
		data, err := os.ReadFile(filepath.Join(root, "main"))
		if err != nil {
			return
		}
		role = strings.TrimSpace(string(data))
		if role != "dri" && role != "steward" {
			return
		}
		clear = func() { _ = os.Remove(filepath.Join(root, "main")) }
	} else {
		if !codexGateIDPattern.MatchString(in.AgentID) {
			return
		}
		role = codexGateChildRole(in.AgentType)
		if role == "" {
			return
		}
		loaded := filepath.Join(root, "loaded", in.AgentID)
		if _, err := os.Stat(loaded); err == nil {
			return
		}
		clear = func() {
			if err := os.MkdirAll(filepath.Dir(loaded), 0o755); err == nil {
				_ = os.WriteFile(loaded, nil, 0o644)
			}
		}
	}
	if in.ToolName == "Bash" && strings.TrimSpace(in.ToolInput.Command) == "ateam learnings "+role {
		clear()
		return
	}
	_ = json.NewEncoder(stdout).Encode(codexHookOutput{HookSpecificOutput: &codexHookSpecificOutput{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: codexGateReason(role),
	}})
}

// codexGatePostCompact re-arms a child after its own compaction.
func codexGatePostCompact(home string, input io.Reader) {
	in, ok := decodeCodexGateInput(input)
	if !ok || !codexGateIDPattern.MatchString(in.AgentID) || codexGateChildRole(in.AgentType) == "" {
		return
	}
	root := codexGateRoot(home, in.SessionID)
	if root == "" {
		return
	}
	_ = os.Remove(filepath.Join(root, "loaded", in.AgentID))
}

// codexGateSessionEnd removes the whole gate root for the session.
func codexGateSessionEnd(home string, input io.Reader) {
	in, ok := decodeCodexGateInput(input)
	if !ok {
		return
	}
	if root := codexGateRoot(home, in.SessionID); root != "" {
		_ = os.RemoveAll(root)
	}
}
