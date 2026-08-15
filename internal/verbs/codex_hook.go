package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

var errCodexHookNoInitiative = errors.New("no Codex initiative for hook cwd")

type codexHookInput struct {
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`
}

type codexHookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

type codexHookOutput struct {
	Decision           string                   `json:"decision,omitempty"`
	Reason             string                   `json:"reason,omitempty"`
	SystemMessage      string                   `json:"systemMessage,omitempty"`
	HookSpecificOutput *codexHookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type codexHookDeps struct {
	resolve func(*cli.Context, string) (bd.Issue, error)
	tie     func(*cli.Context, string, string) error
	unread  func(*cli.Context, string) ([]bd.Issue, error)
	repair  func(*cli.Context, string) error
}

type codexHookKong struct {
	Event string `arg:"" name:"event" help:"Codex hook event adapter."`
}

func RegisterCodexHookKong(p *cli.Parser) {
	p.AddHiddenVerb("codex-hook", "Internal Codex lifecycle hook adapter.", &codexHookKong{})
}

func (c *codexHookKong) Run(ctx *cli.Context) error {
	return runCodexHook(ctx, c.Event, os.Stdin, codexHookDeps{})
}

func runCodexHook(ctx *cli.Context, event string, input io.Reader, deps codexHookDeps) error {
	if ctx == nil {
		return fmt.Errorf("ateam codex-hook: nil context")
	}
	var hookInput codexHookInput
	if err := json.NewDecoder(input).Decode(&hookInput); err != nil {
		return fmt.Errorf("ateam codex-hook: decode input: %w", err)
	}
	if event != "session-start" && event != "user-prompt-submit" && event != "stop" {
		return fmt.Errorf("ateam codex-hook: unsupported event %q", event)
	}
	if event == "stop" && hookInput.StopHookActive {
		return writeCodexHookOutput(ctx, codexHookOutput{})
	}
	if deps.resolve == nil {
		deps.resolve = resolveCodexHookInitiative
	}
	if deps.tie == nil {
		deps.tie = appendSessionID
	}
	if deps.unread == nil {
		deps.unread = queryUnreadMessages
	}
	if deps.repair == nil {
		deps.repair = repairCodexDoorbell
	}
	issue, err := deps.resolve(ctx, hookInput.CWD)
	if errors.Is(err, errCodexHookNoInitiative) {
		return writeCodexHookOutput(ctx, codexHookOutput{})
	}
	if err != nil {
		return writeCodexHookOutput(ctx, codexHookOutput{SystemMessage: "agent-teams Codex hook could not resolve the initiative: " + err.Error()})
	}

	output := codexHookOutput{}
	if event == "session-start" && hookInput.SessionID != "" {
		if err := deps.tie(ctx, issue.ID, hookInput.SessionID); err != nil {
			output.SystemMessage = "agent-teams could not tie this Codex thread to initiative " + issue.ID + ": " + err.Error()
		}
	}
	messages, err := deps.unread(ctx, issue.ID)
	if err != nil {
		output.SystemMessage = "agent-teams could not inspect unread mail for " + issue.ID + ": " + err.Error()
		return writeCodexHookOutput(ctx, output)
	}
	if len(messages) == 0 {
		return writeCodexHookOutput(ctx, output)
	}
	if err := deps.repair(ctx, issue.ID); err != nil && output.SystemMessage == "" {
		output.SystemMessage = "agent-teams could not repair the mail doorbell for " + issue.ID + ": " + err.Error()
	}
	mailPrompt := fmt.Sprintf("You have %d unread agent-teams message(s). Run `ateam mail inbox` now and act on every message.", len(messages))
	switch event {
	case "session-start":
		output.HookSpecificOutput = &codexHookSpecificOutput{HookEventName: "SessionStart", AdditionalContext: mailPrompt}
	case "user-prompt-submit":
		output.HookSpecificOutput = &codexHookSpecificOutput{HookEventName: "UserPromptSubmit", AdditionalContext: mailPrompt}
	case "stop":
		output.Decision = "block"
		output.Reason = mailPrompt
	}
	return writeCodexHookOutput(ctx, output)
}

func resolveCodexHookInitiative(ctx *cli.Context, cwd string) (bd.Issue, error) {
	var issues []bd.Issue
	if err := ctx.BD.RunJSON(&issues, "list", "--status=open", "--json"); err != nil {
		return bd.Issue{}, err
	}
	match := matchByWorktreeOrAncestor(issues, cwd)
	if match == nil {
		return bd.Issue{}, errCodexHookNoInitiative
	}
	issue := *match
	runtimeKind, err := sessionruntime.ResolveStored(initiative.Of(issue).Runtime)
	if err != nil {
		return bd.Issue{}, err
	}
	if runtimeKind != sessionruntime.Codex {
		return bd.Issue{}, errCodexHookNoInitiative
	}
	return issue, nil
}

func repairCodexDoorbell(ctx *cli.Context, initiativeID string) error {
	dir := filepath.Join(ctx.Home, "mailbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return touchFile(filepath.Join(dir, initiativeID+".wake"))
}

func writeCodexHookOutput(ctx *cli.Context, output codexHookOutput) error {
	return json.NewEncoder(ctx.Stdout).Encode(output)
}
