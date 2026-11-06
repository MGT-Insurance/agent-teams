package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// runToolsPath is the Dispatcher route that serves the ui-test tool to a cloud
// run. The run's firewall adds the bearer token; ateam sends none.
const runToolsPath = "/api/run-tools"

const runToolsMaxBody = 10 << 20

// uiRunTools is the run-tools endpoint of the Dispatcher run this process
// runs in.
type uiRunTools struct {
	endpoint string
	// bead is the ui-test bead id, set by wait for the artifact op.
	bead string
}

// uiRunToolsFromEnv returns the endpoint when DSP_BEAD and DSP_API_URL are both
// set. Otherwise ui-test takes the bd path.
func uiRunToolsFromEnv() (*uiRunTools, bool) {
	if _, _, ok := dispatcherRun(); !ok {
		return nil, false
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("DSP_API_URL")), "/")
	if base == "" {
		return nil, false
	}
	return &uiRunTools{endpoint: base + runToolsPath}, true
}

func (rt *uiRunTools) request(body any) (*http.Request, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, rt.endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (rt *uiRunTools) artifactRequest(runID, name string) (*http.Request, error) {
	return rt.request(map[string]string{"op": "artifact", "id": rt.bead, "runId": runID, "name": name})
}

type runToolsFileReq struct {
	Op       string `json:"op"`
	Branch   string `json:"branch"`
	SHA      string `json:"sha"`
	Scope    string `json:"scope"`
	TestUser string `json:"test_user,omitempty"`
	Title    string `json:"title,omitempty"`
	Items    string `json:"items"`
}

// runToolsError turns a non-200 response into a message that names the cause.
func runToolsError(op string, code int, body []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	msg := strings.TrimSpace(e.Error)
	switch code {
	case http.StatusUnauthorized:
		return fmt.Errorf("run-tools %s: 401 not authorized; this run has no ui-test tool, or the run is finished", op)
	case http.StatusConflict:
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

// call posts body and returns the 200 response body. It retries once on a
// network error or a 5xx.
func (rt *uiRunTools) call(d uiTestDeps, op string, body any) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := rt.request(body)
		if err != nil {
			return nil, fmt.Errorf("run-tools %s: %w", op, err)
		}
		resp, err := d.httpDo(req)
		if err != nil {
			lastErr = fmt.Errorf("run-tools %s: %w", op, err)
			continue
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, runToolsMaxBody))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK && rerr == nil:
			return data, nil
		case resp.StatusCode == http.StatusOK:
			return nil, fmt.Errorf("run-tools %s: read: %w", op, rerr)
		case resp.StatusCode >= 500:
			lastErr = runToolsError(op, resp.StatusCode, data)
			continue
		}
		return nil, runToolsError(op, resp.StatusCode, data)
	}
	return nil, lastErr
}

// beadGet reads the bead and its comments in one request.
func (rt *uiRunTools) beadGet(d uiTestDeps, id string) (uiTestBead, []uiTestComment, error) {
	data, err := rt.call(d, "bead_get", map[string]string{"op": "bead_get", "id": id})
	if err != nil {
		return uiTestBead{}, nil, err
	}
	var r struct {
		uiTestBead
		Comments []uiTestComment `json:"comments"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return uiTestBead{}, nil, fmt.Errorf("run-tools bead_get: decode: %w", err)
	}
	sort.SliceStable(r.Comments, func(i, j int) bool { return r.Comments[i].CreatedAt < r.Comments[j].CreatedAt })
	return r.uiTestBead, r.Comments, nil
}

// fileViaRunTools posts the ui_test_file op. It prints the bead id on stdout
// and its url on stderr, so callers that capture stdout still get the id.
func (c *uiTestFileKong) fileViaRunTools(ctx *cli.Context, d uiTestDeps, rt *uiRunTools, branch, sha, items string) error {
	req := runToolsFileReq{Op: "ui_test_file", Branch: branch, SHA: sha, Scope: c.Scope, TestUser: c.TestUser, Title: c.Title, Items: items}
	data, err := rt.call(d, "ui_test_file", req)
	if err != nil {
		return fmt.Errorf("ui-test file: %w", err)
	}
	var r struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal(data, &r); err != nil || r.ID == "" {
		return fmt.Errorf("ui-test file: run-tools ui_test_file: unexpected response")
	}
	fmt.Fprintln(ctx.Stdout, r.ID)
	if r.URL != "" {
		fmt.Fprintln(ctx.Stderr, r.URL)
	}
	if !r.Created {
		fmt.Fprintf(ctx.Stderr, "ui-test file: reused open bead %s (same branch, sha, and items filed recently)\n", r.ID)
	}
	return nil
}
