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
}

type threadResult struct {
	Thread appServerThread `json:"thread"`
}

type turnResult struct {
	Turn appServerTurn `json:"turn"`
}

// turnsPage is the response shape of thread/turns/list: a page of turn
// metadata (no items, when requested with itemsView "notLoaded") plus an
// opaque cursor to fetch the next page.
type turnsPage struct {
	Data       []appServerTurn `json:"data"`
	NextCursor *string         `json:"nextCursor"`
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

	resumeParams := map[string]any{
		"threadId": session.ID,
		// Return thread metadata and live-resume state only; do not hydrate
		// thread.turns with the full rollout history. A long-running session
		// exceeds the app-server's 4MB read limit if we hydrate it here. The
		// in-progress turn (if any) is resolved separately via the paginated
		// thread/turns/list, which returns metadata only.
		"excludeTurns": true,
	}
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

	activeTurnID, err := resolveActiveTurnID(ctx, client, session.ID)
	if err != nil {
		return fmt.Errorf("codex resume: list turns: %w", err)
	}

	statusType := resumed.Thread.Status.Type
	if statusType == "active" {
		if activeTurnID == "" {
			return fmt.Errorf("codex resume: thread %s is active but app-server returned no in-progress turn", session.ID)
		}
		if err := steerCodexTurn(ctx, client, session.ID, activeTurnID, req.Prompt); err != nil {
			return fmt.Errorf("codex resume: %w", err)
		}
		return nil
	}
	if activeTurnID != "" {
		return fmt.Errorf("codex resume: thread %s reports %s with in-progress turn %s", session.ID, statusType, activeTurnID)
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

// maxTurnsListPages bounds thread/turns/list pagination so a malformed or
// looping cursor from the app-server cannot hang resume indefinitely. Real
// threads page through in the tens to low hundreds of turns at most.
const maxTurnsListPages = 10000

// resolveActiveTurnID finds the in-progress turn for a thread, if any, using
// only paginated turn metadata (id + status, no items) so the lookup stays
// small regardless of how much conversation history the thread carries. It
// preserves the exact invariant the old full-history scan used: the most
// recent turn whose status is "inProgress" wins, without assuming the
// newest turn returned is necessarily the active one.
func resolveActiveTurnID(ctx context.Context, client appServerRPC, threadID string) (string, error) {
	var cursor *string
	for page := 0; page < maxTurnsListPages; page++ {
		params := map[string]any{
			"threadId":      threadID,
			"sortDirection": "desc",
			"itemsView":     "notLoaded",
		}
		if cursor != nil {
			params["cursor"] = *cursor
		}
		var result turnsPage
		if err := client.Request(ctx, "thread/turns/list", params, &result); err != nil {
			return "", err
		}
		if id := activeTurn(result.Data); id != "" {
			return id, nil
		}
		if result.NextCursor == nil || *result.NextCursor == "" {
			return "", nil
		}
		cursor = result.NextCursor
	}
	return "", fmt.Errorf("thread %s: exceeded %d thread/turns/list pages without exhausting turns", threadID, maxTurnsListPages)
}

// activeTurn scans turns (ordered newest-first, as thread/turns/list returns
// with sortDirection "desc") and returns the ID of the first one whose status
// is "inProgress" — the most recent in-progress turn, if any.
func activeTurn(turns []appServerTurn) string {
	for _, turn := range turns {
		if turn.Status == "inProgress" {
			return turn.ID
		}
	}
	return ""
}
