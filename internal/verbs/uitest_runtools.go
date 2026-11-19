package verbs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

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
	Op       string   `json:"op"`
	Branch   string   `json:"branch"`
	SHA      string   `json:"sha"`
	Scope    string   `json:"scope"`
	TestUser string   `json:"test_user,omitempty"`
	Type     string   `json:"type,omitempty"`
	Needs    []string `json:"needs,omitempty"`
	Title    string   `json:"title,omitempty"`
	Items    string   `json:"items"`
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

// beadReply posts the answer as a comment on the bead and returns the bead's
// status after the write.
func (rt *uiRunTools) beadReply(d uiTestDeps, id, text string) (string, error) {
	data, err := rt.call(d, "bead_reply", map[string]string{"op": "bead_reply", "id": id, "text": text})
	if err != nil {
		return "", err
	}
	var r struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &r); err != nil || r.Status == "" {
		return "", fmt.Errorf("run-tools bead_reply: unexpected response")
	}
	return r.Status, nil
}

// sameNeeds reports whether got holds exactly the values in want, in any order.
func sameNeeds(want, got []string) bool {
	if len(want) != len(got) {
		return false
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			return false
		}
	}
	return true
}

// fileViaRunTools posts the ui_test_file op, sending type only when it is not
// ui-test and failing closed (exit 5) when the response does not echo the
// type. It prints the bead id on stdout
// and its url on stderr, so callers that capture stdout still get the id.
func (c *uiTestFileKong) fileViaRunTools(ctx *cli.Context, d uiTestDeps, rt *uiRunTools, job string, needs []string, branch, sha, items string) error {
	req := runToolsFileReq{Op: "ui_test_file", Branch: branch, SHA: sha, Scope: c.Scope, TestUser: c.TestUser, Title: c.Title, Items: items, Needs: needs}
	if job != uiJobUI {
		req.Type = job
	}
	data, err := rt.CallAs(d.httpDo, "ui_test_file", job, req, 1)
	if err != nil {
		return fmt.Errorf("live-test file: %w", err)
	}
	var r struct {
		ID      string   `json:"id"`
		URL     string   `json:"url"`
		Created bool     `json:"created"`
		Type    string   `json:"type"`
		Needs   []string `json:"needs"`
	}
	if err := json.Unmarshal(data, &r); err != nil || r.ID == "" {
		return fmt.Errorf("live-test file: run-tools ui_test_file: unexpected response")
	}
	// A current Dispatcher always echoes the effective type. An older one
	// strips the unknown type key, echoes nothing, and files a ui-test bead.
	if job != uiJobUI && r.Type != job {
		filed := r.Type
		if filed == "" {
			filed = uiJobUI
		}
		fmt.Fprintf(ctx.Stderr, "Dispatcher does not support --job %s yet; test locally\n", job)
		fmt.Fprintf(ctx.Stderr, "live-test file: bead %s was filed as %s, not %s; ignore its verdict\n", r.ID, filed, job)
		return cli.Silent(uiExitIneligible)
	}
	if job == uiJobUI && r.Type != "" && r.Type != job {
		return fmt.Errorf("live-test file: bead %s was filed as %s, not %s; ignore its verdict", r.ID, r.Type, job)
	}
	// A current Dispatcher echoes the needs it received. An older one strips
	// the unknown key and runs the test on the shared main staging tenant.
	if len(needs) > 0 && !sameNeeds(needs, r.Needs) {
		fmt.Fprintf(ctx.Stderr, "Dispatcher does not support --needs %s yet; test locally\n", strings.Join(needs, ","))
		fmt.Fprintf(ctx.Stderr, "live-test file: bead %s was filed without those needs; ignore its verdict\n", r.ID)
		return cli.Silent(uiExitIneligible)
	}
	fmt.Fprintln(ctx.Stdout, r.ID)
	if r.URL != "" {
		fmt.Fprintln(ctx.Stderr, r.URL)
	}
	if !r.Created {
		fmt.Fprintf(ctx.Stderr, "live-test file: reused open bead %s (same branch, sha, and items filed recently)\n", r.ID)
	}
	return nil
}
