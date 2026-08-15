package sessionruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const appServerReadLimit = 4 << 20

type appServerRPC interface {
	Request(context.Context, string, any, any) error
	Notify(context.Context, string, any) error
	Close() error
}

type appServerDialFunc func(context.Context, string, io.Writer) (appServerRPC, error)

type websocketRPC struct {
	conn   *websocket.Conn
	events io.Writer

	mu     sync.Mutex
	nextID int64
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcEnvelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

func dialManagedAppServer(ctx context.Context, socketPath string, events io.Writer) (appServerRPC, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("codex app-server: empty socket path")
	}
	dialer := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	client := &http.Client{Transport: transport}
	conn, response, err := websocket.Dial(ctx, "ws://codex-app-server/", &websocket.DialOptions{
		HTTPClient:      client,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("codex app-server: connect to %s: HTTP %s: %w", socketPath, response.Status, err)
		}
		return nil, fmt.Errorf("codex app-server: connect to %s: %w", socketPath, err)
	}
	conn.SetReadLimit(appServerReadLimit)
	if events == nil {
		events = io.Discard
	}
	return &websocketRPC{conn: conn, events: events}, nil
}

func (c *websocketRPC) Request(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextID++
	id := c.nextID
	request := map[string]any{"id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	if err := wsjson.Write(ctx, c.conn, request); err != nil {
		return fmt.Errorf("codex app-server: write %s request: %w", method, err)
	}

	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, c.conn, &raw); err != nil {
			return fmt.Errorf("codex app-server: read %s response: %w", method, err)
		}
		if _, err := c.events.Write(append(append([]byte(nil), raw...), '\n')); err != nil {
			return fmt.Errorf("codex app-server: write event log: %w", err)
		}
		var envelope rpcEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("codex app-server: decode response: %w", err)
		}
		if string(envelope.ID) != strconv.FormatInt(id, 10) {
			continue
		}
		if envelope.Error != nil {
			return fmt.Errorf("codex app-server: %s: RPC %d: %s", method, envelope.Error.Code, envelope.Error.Message)
		}
		if result == nil || len(envelope.Result) == 0 || string(envelope.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("codex app-server: decode %s result: %w", method, err)
		}
		return nil
	}
}

func (c *websocketRPC) Notify(ctx context.Context, method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	message := map[string]any{"method": method}
	if params != nil {
		message["params"] = params
	}
	if err := wsjson.Write(ctx, c.conn, message); err != nil {
		return fmt.Errorf("codex app-server: write %s notification: %w", method, err)
	}
	return nil
}

func (c *websocketRPC) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}
