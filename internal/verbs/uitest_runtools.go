package verbs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/runtools"
)

// uiRunTools is the run-tools endpoint of the Dispatcher run this process
// runs in.
type uiRunTools struct {
	*runtools.Client
	// bead is the ui-test bead id, set by wait for the artifact op.
	bead string
}

// uiRunToolsFromEnv returns the endpoint when DSP_BEAD and DSP_API_URL are both
// set. Otherwise ui-test takes the bd path.
func uiRunToolsFromEnv() (*uiRunTools, bool) {
	c, ok := runtools.FromEnv()
	if !ok {
		return nil, false
	}
	return &uiRunTools{Client: c}, true
}

func (rt *uiRunTools) artifactRequest(runID, name string) (*http.Request, error) {
	return rt.Request(map[string]string{"op": "artifact", "id": rt.bead, "runId": runID, "name": name})
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

// call posts body and returns the 200 response body. It retries once on a
// network error or a 5xx.
func (rt *uiRunTools) call(d uiTestDeps, op string, body any) ([]byte, error) {
	return rt.Call(d.httpDo, op, body, 1)
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
