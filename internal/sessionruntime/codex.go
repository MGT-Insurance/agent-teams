package sessionruntime

import (
	"context"
	"fmt"
	"io"
)

const (
	appServerServiceName   = "agent_teams"
	appServerClientVersion = "1.0.0"
)

// CodexAdapter starts or reconnects to the standalone Codex managed
// app-server, then submits one turn. The daemon owns the turn after the RPC is
// accepted; the adapter is deliberately short-lived and safe to disconnect.
type CodexAdapter struct {
	Executable string
	SocketPath string

	ensureDaemon ensureDaemonFunc
	dial         appServerDialFunc
}

func (a CodexAdapter) Kind() Kind { return Codex }

type appServerThreadStatus struct {
	Type string `json:"type"`
}

type appServerTurn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type appServerThread struct {
	ID     string                `json:"id"`
	Status appServerThreadStatus `json:"status"`
	Turns  []appServerTurn       `json:"turns,omitempty"`
}

type threadResult struct {
	Thread appServerThread `json:"thread"`
}

type turnResult struct {
	Turn appServerTurn `json:"turn"`
}

func (a CodexAdapter) Launch(ctx context.Context, req Request, sink SessionSink) error {
	if sink == nil {
		return fmt.Errorf("codex launch: nil session sink")
	}
	if err := validateCodexRequest(req); err != nil {
		return err
	}
	client, err := a.connect(ctx, req.Events)
	if err != nil {
		return fmt.Errorf("codex launch: %w", err)
	}
	defer client.Close()

	params := map[string]any{
		"cwd":            req.Worktree,
		"approvalPolicy": "never",
		"sandbox":        "danger-full-access",
		"serviceName":    appServerServiceName,
	}
	if req.AgentTeamsHome != "" {
		params["config"] = codexThreadConfig(req.AgentTeamsHome)
	}
	if req.Model != "" {
		params["model"] = req.Model
	}
	var started threadResult
	if err := client.Request(ctx, "thread/start", params, &started); err != nil {
		return fmt.Errorf("codex launch: %w", err)
	}
	if started.Thread.ID == "" {
		return fmt.Errorf("codex launch: app-server returned an empty thread id")
	}
	ref := SessionRef{Runtime: Codex, ID: started.Thread.ID}
	if err := sink(ref); err != nil {
		return fmt.Errorf("codex launch: bind thread %s: %w", ref.ID, err)
	}
	if _, err := startCodexTurn(ctx, client, req, ref.ID); err != nil {
		return fmt.Errorf("codex launch: %w", err)
	}
	return nil
}

func (a CodexAdapter) Resume(ctx context.Context, req Request, session SessionRef) error {
	if session.Runtime != Codex {
		return fmt.Errorf("codex resume: session runtime is %q", session.Runtime)
	}
	if session.ID == "" {
		return fmt.Errorf("codex resume: empty session id")
	}
	if err := validateCodexRequest(req); err != nil {
		return err
	}
	client, err := a.connect(ctx, req.Events)
	if err != nil {
		return fmt.Errorf("codex resume: %w", err)
	}
	defer client.Close()

	resumeParams := map[string]any{"threadId": session.ID}
	if req.AgentTeamsHome != "" {
		// Re-apply the sticky override on every resume. Besides making the
		// contract explicit, this repairs threads first created by an older
		// ateam that relied on the managed daemon's process environment.
		resumeParams["config"] = codexThreadConfig(req.AgentTeamsHome)
	}
	if req.Model != "" {
		resumeParams["model"] = req.Model
	}
	var resumed threadResult
	if err := client.Request(ctx, "thread/resume", resumeParams, &resumed); err != nil {
		return fmt.Errorf("codex resume: %w", err)
	}
	if resumed.Thread.ID != session.ID {
		return fmt.Errorf("codex resume: requested thread %s but app-server returned %s", session.ID, resumed.Thread.ID)
	}

	var read threadResult
	if err := client.Request(ctx, "thread/read", map[string]any{
		"threadId":     session.ID,
		"includeTurns": true,
	}, &read); err != nil {
		return fmt.Errorf("codex resume: inspect thread: %w", err)
	}
	if read.Thread.ID != session.ID {
		return fmt.Errorf("codex resume: read returned thread %s, want %s", read.Thread.ID, session.ID)
	}

	activeTurnID := activeTurn(read.Thread)
	if read.Thread.Status.Type == "active" {
		if activeTurnID == "" {
			return fmt.Errorf("codex resume: thread %s is active but app-server returned no in-progress turn", session.ID)
		}
		if err := steerCodexTurn(ctx, client, session.ID, activeTurnID, req.Prompt); err != nil {
			return fmt.Errorf("codex resume: %w", err)
		}
		return nil
	}
	if activeTurnID != "" {
		return fmt.Errorf("codex resume: thread %s reports %s with in-progress turn %s", session.ID, read.Thread.Status.Type, activeTurnID)
	}
	if _, err := startCodexTurn(ctx, client, req, session.ID); err != nil {
		return fmt.Errorf("codex resume: %w", err)
	}
	return nil
}

func codexThreadConfig(agentTeamsHome string) map[string]any {
	return map[string]any{
		"shell_environment_policy": map[string]any{
			"set": map[string]string{"AGENT_TEAMS_HOME": agentTeamsHome},
		},
	}
}

func validateCodexRequest(req Request) error {
	if req.Worktree == "" {
		return fmt.Errorf("codex: empty worktree")
	}
	if req.Prompt == "" {
		return fmt.Errorf("codex: empty prompt")
	}
	return nil
}

func (a CodexAdapter) connect(ctx context.Context, events io.Writer) (appServerRPC, error) {
	ensure := a.ensureDaemon
	if ensure == nil {
		ensure = ensureManagedCodexDaemon
	}
	info, err := ensure(ctx, a.Executable)
	if err != nil {
		return nil, err
	}
	socketPath := a.SocketPath
	if socketPath == "" {
		socketPath = info.SocketPath
	}
	dial := a.dial
	if dial == nil {
		dial = dialManagedAppServer
	}
	client, err := dial(ctx, socketPath, events)
	if err != nil {
		return nil, err
	}
	var initialized map[string]any
	if err := client.Request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    appServerServiceName,
			"title":   "agent-teams",
			"version": appServerClientVersion,
		},
	}, &initialized); err != nil {
		client.Close()
		return nil, err
	}
	if err := client.Notify(ctx, "initialized", map[string]any{}); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func startCodexTurn(ctx context.Context, client appServerRPC, req Request, threadID string) (string, error) {
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type": "text",
			"text": req.Prompt,
		}},
		"cwd":            req.Worktree,
		"approvalPolicy": "never",
		"sandboxPolicy":  map[string]any{"type": "dangerFullAccess"},
	}
	if req.Model != "" {
		params["model"] = req.Model
	}
	var started turnResult
	if err := client.Request(ctx, "turn/start", params, &started); err != nil {
		return "", err
	}
	if started.Turn.ID == "" {
		return "", fmt.Errorf("turn/start returned an empty turn id")
	}
	return started.Turn.ID, nil
}

func steerCodexTurn(ctx context.Context, client appServerRPC, threadID, turnID, prompt string) error {
	var result struct {
		TurnID string `json:"turnId"`
	}
	if err := client.Request(ctx, "turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input": []map[string]any{{
			"type": "text",
			"text": prompt,
		}},
	}, &result); err != nil {
		return err
	}
	if result.TurnID != turnID {
		return fmt.Errorf("turn/steer returned turn %s, want %s", result.TurnID, turnID)
	}
	return nil
}

func activeTurn(thread appServerThread) string {
	for i := len(thread.Turns) - 1; i >= 0; i-- {
		if thread.Turns[i].Status == "inProgress" {
			return thread.Turns[i].ID
		}
	}
	return ""
}
