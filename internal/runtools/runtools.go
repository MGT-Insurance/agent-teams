// Package runtools is the client of the Dispatcher run-tools route
// (POST /api/run-tools), the HTTP endpoint a cloud run uses for the tools its
// job grants: ui-test, telegram, and pages. The run's firewall adds the bearer token,
// so this client sends none. Both the ui-test verbs and the dispatcher
// transport build on it, so there is one client and one error mapping.
package runtools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Path is the Dispatcher route that serves run tools to a cloud run.
const Path = "/api/run-tools"

// MaxBody caps the response bytes Call reads.
const MaxBody = 10 << 20

// Do performs one HTTP request. The Do method of *http.Client satisfies it.
type Do func(*http.Request) (*http.Response, error)

// Client addresses the run-tools endpoint of the Dispatcher run this process
// runs in.
type Client struct {
	Endpoint string
}

// FromEnv returns the client when DSP_BEAD and DSP_API_URL are both set.
// Otherwise the process is not in a Dispatcher run and ok is false.
func FromEnv() (*Client, bool) {
	if os.Getenv("DSP_BEAD") == "" {
		return nil, false
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("DSP_API_URL")), "/")
	if base == "" {
		return nil, false
	}
	return &Client{Endpoint: base + Path}, true
}

// Request builds the JSON POST for body.
func (c *Client) Request(body any) (*http.Request, error) {
	// Encode without HTML escaping: a page body is mostly < > &, and the
	// default escaping inflates each to six bytes against the server's body cap.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.Endpoint, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// toolFamily names the tool a run-tools op needs, for the 401 text.
func toolFamily(op string) string {
	if strings.HasPrefix(op, "telegram_") {
		return "telegram"
	}
	if strings.HasPrefix(op, "page_") {
		return "pages"
	}
	return "ui-test"
}

// Error turns a non-200 response into a message that names the cause.
func Error(op string, code int, body []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	msg := strings.TrimSpace(e.Error)
	switch {
	case code == http.StatusUnauthorized:
		return fmt.Errorf("run-tools %s: 401 not authorized; this run's job has no %s tool, or the run is finished", op, toolFamily(op))
	case code == http.StatusConflict && toolFamily(op) == "ui-test":
		if msg == "" {
			msg = "ui-test cap reached for this run, or budget refused"
		}
		return fmt.Errorf("run-tools %s: 409 %s", op, msg)
	}
	if msg == "" {
		msg = "no error message"
	}
	return fmt.Errorf("run-tools %s: HTTP %d: %s", op, code, msg)
}

// Call posts body through do and returns the 200 response body. It retries up
// to retries more times after a network error or a 5xx, and never after a 4xx.
func (c *Client) Call(do Do, op string, body any, retries int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		req, err := c.Request(body)
		if err != nil {
			return nil, fmt.Errorf("run-tools %s: %w", op, err)
		}
		resp, err := do(req)
		if err != nil {
			lastErr = fmt.Errorf("run-tools %s: %w", op, err)
			continue
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK && rerr == nil:
			return data, nil
		case resp.StatusCode == http.StatusOK:
			return nil, fmt.Errorf("run-tools %s: read: %w", op, rerr)
		case resp.StatusCode >= 500:
			lastErr = Error(op, resp.StatusCode, data)
			continue
		}
		return nil, Error(op, resp.StatusCode, data)
	}
	return nil, lastErr
}
