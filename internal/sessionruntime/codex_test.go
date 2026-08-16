package sessionruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type rpcCall struct {
	Method string
	Params map[string]any
}

type fakeAppServer struct {
	calls         []rpcCall
	notifications []rpcCall
	handle        func(string, map[string]any) (any, error)
	closed        bool
}

func (f *fakeAppServer) Request(_ context.Context, method string, params, result any) error {
	values := map[string]any{}
	if params != nil {
		data, _ := json.Marshal(params)
		_ = json.Unmarshal(data, &values)
	}
	f.calls = append(f.calls, rpcCall{Method: method, Params: values})
	value, err := f.handle(method, values)
	if err != nil {
		return err
	}
	if result != nil {
		data, _ := json.Marshal(value)
		return json.Unmarshal(data, result)
	}
	return nil
}

func (f *fakeAppServer) Notify(_ context.Context, method string, params any) error {
	f.notifications = append(f.notifications, rpcCall{Method: method})
	return nil
}

func (f *fakeAppServer) Close() error {
	f.closed = true
	return nil
}

func testAdapter(server *fakeAppServer) CodexAdapter {
	return CodexAdapter{
		ensureDaemon: func(context.Context, string) (ManagedDaemonInfo, error) {
			return ManagedDaemonInfo{
				Status:           "alreadyRunning",
				ManagedCodexPath: "/standalone/codex",
				SocketPath:       "/socket",
			}, nil
		},
		dial: func(_ context.Context, socketPath string, _ io.Writer) (appServerRPC, error) {
			if socketPath != "/socket" {
				return nil, fmt.Errorf("socket = %q", socketPath)
			}
			return server, nil
		},
	}
}

func TestCodexAdapterLaunchStartsThreadBindsThenStartsTurn(t *testing.T) {
	server := &fakeAppServer{handle: func(method string, params map[string]any) (any, error) {
		switch method {
		case "initialize":
			return map[string]any{"platformFamily": "unix"}, nil
		case "thread/start":
			if params["cwd"] != "/worktree" || params["model"] != "gpt-test" {
				t.Fatalf("thread/start params = %#v", params)
			}
			config := params["config"].(map[string]any)
			policy := config["shell_environment_policy"].(map[string]any)
			set := policy["set"].(map[string]any)
			if set["AGENT_TEAMS_HOME"] != "/workspace" {
				t.Fatalf("thread/start config = %#v", config)
			}
			return map[string]any{"thread": map[string]any{"id": "thread-123", "status": map[string]any{"type": "idle"}}}, nil
		case "turn/start":
			if params["threadId"] != "thread-123" || params["cwd"] != "/worktree" || params["model"] != "gpt-test" {
				t.Fatalf("turn/start params = %#v", params)
			}
			input := params["input"].([]any)
			if input[0].(map[string]any)["text"] != "$dri at-1" {
				t.Fatalf("input = %#v", input)
			}
			return map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}}, nil
		default:
			return nil, fmt.Errorf("unexpected method %s", method)
		}
	}}
	var bound SessionRef
	err := testAdapter(server).Launch(context.Background(), Request{
		InitiativeID:   "at-1",
		AgentTeamsHome: "/workspace",
		Worktree:       "/worktree",
		Prompt:         "$dri at-1",
		Model:          "gpt-test",
	}, func(ref SessionRef) error {
		bound = ref
		if len(server.calls) != 2 || server.calls[1].Method != "thread/start" {
			t.Fatalf("turn started before bind: %#v", server.calls)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if bound != (SessionRef{Runtime: Codex, ID: "thread-123"}) {
		t.Fatalf("bound = %+v", bound)
	}
	if got := callMethods(server.calls); got != "initialize,thread/start,turn/start" {
		t.Fatalf("calls = %s", got)
	}
	if len(server.notifications) != 1 || server.notifications[0].Method != "initialized" {
		t.Fatalf("notifications = %#v", server.notifications)
	}
	if !server.closed {
		t.Fatal("client was not closed")
	}
}

func TestCodexAdapterResumeStartsIdleThread(t *testing.T) {
	server := resumeServer(t, "idle", nil)
	err := testAdapter(server).Resume(context.Background(), Request{
		AgentTeamsHome: "/workspace",
		Worktree:       "/worktree",
		Prompt:         "wake",
	}, SessionRef{Runtime: Codex, ID: "thread-123"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := callMethods(server.calls); got != "initialize,thread/resume,thread/read,turn/start" {
		t.Fatalf("calls = %s", got)
	}
	config := server.calls[1].Params["config"].(map[string]any)
	policy := config["shell_environment_policy"].(map[string]any)
	set := policy["set"].(map[string]any)
	if set["AGENT_TEAMS_HOME"] != "/workspace" {
		t.Fatalf("thread/resume config = %#v", config)
	}
}

func TestCodexAdapterResumeSteersActualActiveTurn(t *testing.T) {
	server := resumeServer(t, "active", []map[string]any{
		{"id": "turn-done", "status": "completed"},
		{"id": "turn-live", "status": "inProgress"},
	})
	err := testAdapter(server).Resume(context.Background(), Request{
		Worktree: "/worktree",
		Prompt:   "new mail",
	}, SessionRef{Runtime: Codex, ID: "thread-123"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := callMethods(server.calls); got != "initialize,thread/resume,thread/read,turn/steer" {
		t.Fatalf("calls = %s", got)
	}
	steer := server.calls[len(server.calls)-1].Params
	if steer["expectedTurnId"] != "turn-live" {
		t.Fatalf("steer params = %#v", steer)
	}
}

func TestCodexAdapterEnsuresDaemonAndReconnectsForEveryDelivery(t *testing.T) {
	servers := []*fakeAppServer{
		resumeServer(t, "idle", nil),
		resumeServer(t, "idle", nil),
	}
	ensureCalls := 0
	dialCalls := 0
	adapter := CodexAdapter{
		ensureDaemon: func(context.Context, string) (ManagedDaemonInfo, error) {
			ensureCalls++
			status := "alreadyRunning"
			if ensureCalls == 2 {
				// The second short-lived delivery observes that Codex had to
				// restart its managed daemon. The adapter uses the same path.
				status = "started"
			}
			return ManagedDaemonInfo{Status: status, ManagedCodexPath: "/standalone/codex", SocketPath: "/socket"}, nil
		},
		dial: func(context.Context, string, io.Writer) (appServerRPC, error) {
			server := servers[dialCalls]
			dialCalls++
			return server, nil
		},
	}
	request := Request{Worktree: "/worktree", Prompt: "mail"}
	ref := SessionRef{Runtime: Codex, ID: "thread-123"}
	if err := adapter.Resume(context.Background(), request, ref); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := adapter.Resume(context.Background(), request, ref); err != nil {
		t.Fatalf("delivery after daemon restart: %v", err)
	}
	if ensureCalls != 2 || dialCalls != 2 {
		t.Fatalf("ensure calls = %d, dial calls = %d; want 2 each", ensureCalls, dialCalls)
	}
	for i, server := range servers {
		if !server.closed {
			t.Fatalf("client %d was not closed", i)
		}
	}
}

func TestCodexAdapterFailures(t *testing.T) {
	t.Run("bind failure prevents turn", func(t *testing.T) {
		server := &fakeAppServer{handle: func(method string, _ map[string]any) (any, error) {
			switch method {
			case "initialize":
				return map[string]any{}, nil
			case "thread/start":
				return map[string]any{"thread": map[string]any{"id": "thread-123"}}, nil
			default:
				return nil, fmt.Errorf("unexpected method %s", method)
			}
		}}
		err := testAdapter(server).Launch(context.Background(), Request{Worktree: "/w", Prompt: "p"}, func(SessionRef) error {
			return fmt.Errorf("beads unavailable")
		})
		if err == nil || !strings.Contains(err.Error(), "beads unavailable") {
			t.Fatalf("error = %v", err)
		}
		if got := callMethods(server.calls); got != "initialize,thread/start" {
			t.Fatalf("calls = %s", got)
		}
	})

	t.Run("resume mismatch", func(t *testing.T) {
		server := &fakeAppServer{handle: func(method string, _ map[string]any) (any, error) {
			switch method {
			case "initialize":
				return map[string]any{}, nil
			case "thread/resume":
				return map[string]any{"thread": map[string]any{"id": "wrong"}}, nil
			default:
				return nil, fmt.Errorf("unexpected method %s", method)
			}
		}}
		err := testAdapter(server).Resume(context.Background(), Request{Worktree: "/w", Prompt: "p"}, SessionRef{Runtime: Codex, ID: "wanted"})
		if err == nil || !strings.Contains(err.Error(), "returned wrong") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("active without turn fails closed", func(t *testing.T) {
		server := resumeServer(t, "active", nil)
		err := testAdapter(server).Resume(context.Background(), Request{Worktree: "/w", Prompt: "p"}, SessionRef{Runtime: Codex, ID: "thread-123"})
		if err == nil || !strings.Contains(err.Error(), "no in-progress turn") {
			t.Fatalf("error = %v", err)
		}
	})
}

func resumeServer(t *testing.T, status string, turns []map[string]any) *fakeAppServer {
	t.Helper()
	return &fakeAppServer{handle: func(method string, params map[string]any) (any, error) {
		switch method {
		case "initialize":
			return map[string]any{}, nil
		case "thread/resume":
			if params["threadId"] != "thread-123" {
				t.Fatalf("thread/resume params = %#v", params)
			}
			return map[string]any{"thread": map[string]any{"id": "thread-123", "status": map[string]any{"type": status}}}, nil
		case "thread/read":
			return map[string]any{"thread": map[string]any{
				"id": "thread-123", "status": map[string]any{"type": status}, "turns": turns,
			}}, nil
		case "turn/start":
			return map[string]any{"turn": map[string]any{"id": "turn-new", "status": "inProgress"}}, nil
		case "turn/steer":
			return map[string]any{"turnId": params["expectedTurnId"]}, nil
		default:
			return nil, fmt.Errorf("unexpected method %s", method)
		}
	}}
}

func callMethods(calls []rpcCall) string {
	var methods []string
	for _, call := range calls {
		methods = append(methods, call.Method)
	}
	return strings.Join(methods, ",")
}

func TestWebsocketRPCHandlesInterleavedNotification(t *testing.T) {
	server := startUnixWebsocketServer(t, func(ctx context.Context, conn *websocket.Conn) {
		var request map[string]any
		if err := wsjson.Read(ctx, conn, &request); err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		if err := wsjson.Write(ctx, conn, map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": "t"}}}); err != nil {
			t.Errorf("server notify: %v", err)
			return
		}
		if err := wsjson.Write(ctx, conn, map[string]any{"id": request["id"], "result": map[string]any{"ok": true}}); err != nil {
			t.Errorf("server response: %v", err)
		}
	})
	var events bytes.Buffer
	client, err := dialManagedAppServer(context.Background(), server, &events)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	var result struct {
		OK bool `json:"ok"`
	}
	if err := client.Request(context.Background(), "test", map[string]any{"x": 1}, &result); err != nil {
		t.Fatalf("request: %v", err)
	}
	if !result.OK {
		t.Fatal("missing result")
	}
	if strings.Count(events.String(), "\n") != 2 || !strings.Contains(events.String(), "thread/started") {
		t.Fatalf("events = %q", events.String())
	}
}

func startUnixWebsocketServer(t *testing.T, serve func(context.Context, *websocket.Conn)) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("/tmp", "ateam-ws-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	socketPath := filepath.Join(tmpDir, "app-server.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		serve(ctx, conn)
	})
	server := &http.Server{Handler: handler}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return socketPath
}
