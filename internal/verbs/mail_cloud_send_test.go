package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

const (
	testCloudInit = "at-cloud1"
	testRunBead   = "eri-run1"
)

// cloudSendEnv wires fakes for every bd and exec call trySendToCloud makes.
type cloudSendEnv struct {
	ctx    *cli.Context
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	file   string

	localCalls    int
	localIssue    string // JSON array returned for `show` on the laptop
	cloudInit     string // JSON array for the initiative in cloud_home
	cloudInitErr  error
	runBeadJSON   string
	comments      string
	cloudBDHomes  []string
	commentArgs   []string
	commentText   string
	commentCalls  int
	sqlQuery      string
	sqlOut        string // rows JSON for the post-comment query
	sqlErr        error
	headOut       string
	headErr       error
	cloudHome     string
	dispatcherDir string
}

func newCloudSendEnv(t *testing.T, configure bool) *cloudSendEnv {
	t.Helper()
	e := &cloudSendEnv{
		stdout:        &bytes.Buffer{},
		stderr:        &bytes.Buffer{},
		localIssue:    "[]",
		cloudInit:     issueJSON(bd.Issue{ID: testCloudInit, Status: "open", Description: "where: run:" + testRunBead}),
		runBeadJSON:   issueJSON(bd.Issue{ID: testRunBead, Status: "open", Description: "type: ateam-dri\nmeta-initiative: " + testCloudInit + "\n---\nproblem"}),
		comments:      "[]",
		sqlOut:        `[{"seq": 43}]`,
		headOut:       `[{"head": 42}]`,
		cloudHome:     t.TempDir(),
		dispatcherDir: t.TempDir(),
	}
	t.Setenv("AGENT_TEAMS_HOST", "testhost")
	home := t.TempDir()
	if configure {
		cfg := "cloud_home = \"" + e.cloudHome + "\"\ndispatcher_beads = \"" + e.dispatcherDir + "\"\n"
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.file = filepath.Join(t.TempDir(), "msg.txt")
	if err := os.WriteFile(e.file, []byte("hello cloud"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.ctx = &cli.Context{
		Home: home,
		BD: &fakeBD{runFn: func(args ...string) (string, error) {
			e.localCalls++
			return e.localIssue, nil
		}},
		Stdout: e.stdout,
		Stderr: e.stderr,
	}

	oldBD, oldComment, oldSQL := cloudSendBD, cloudSendComment, cloudSendSQL
	t.Cleanup(func() { cloudSendBD, cloudSendComment, cloudSendSQL = oldBD, oldComment, oldSQL })
	cloudSendBD = func(h string) cli.BDRunner {
		e.cloudBDHomes = append(e.cloudBDHomes, h)
		return &fakeBD{runFn: func(args ...string) (string, error) {
			switch {
			case h == e.cloudHome && args[0] == "show":
				return e.cloudInit, e.cloudInitErr
			case h == e.dispatcherDir && args[0] == "show":
				return e.runBeadJSON, nil
			case h == e.dispatcherDir && args[0] == "comments":
				return e.comments, nil
			}
			t.Errorf("unexpected bd call on %s: %v", h, args)
			return "", errors.New("unexpected")
		}}
	}
	cloudSendComment = func(dir, actor, runBead, file string) error {
		e.commentCalls++
		e.commentArgs = []string{dir, actor, runBead}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		e.commentText = string(raw)
		return nil
	}
	cloudSendSQL = func(dir, query string) (string, error) {
		if query == journalHeadQuery {
			return e.headOut, e.headErr
		}
		e.sqlQuery = query
		return e.sqlOut, e.sqlErr
	}
	return e
}

func (e *cloudSendEnv) send(recipient, sender string) (bool, error) {
	c := &sendKong{RecipientID: recipient, File: e.file}
	return c.trySendToCloud(e.ctx, sender)
}

func TestCloudSendNoCloudHomeMakesNoBDCall(t *testing.T) {
	e := newCloudSendEnv(t, false)
	handled, err := e.send(testCloudInit, "at-me1")
	if handled || err != nil {
		t.Fatalf("got (%t, %v), want (false, nil)", handled, err)
	}
	if e.localCalls != 0 || len(e.cloudBDHomes) != 0 || e.commentCalls != 0 {
		t.Errorf("bd calls: local=%d cloud=%v comments=%d, want none", e.localCalls, e.cloudBDHomes, e.commentCalls)
	}
}

func TestCloudSendLocalRecipientUsesExistingPath(t *testing.T) {
	e := newCloudSendEnv(t, true)
	for _, r := range []string{StewardHandle, LocalStewardHandle} {
		if handled, err := e.send(r, "at-me1"); handled || err != nil {
			t.Errorf("%s: got (%t, %v), want (false, nil)", r, handled, err)
		}
	}
	if e.localCalls != 0 || len(e.cloudBDHomes) != 0 {
		t.Errorf("steward recipients made bd calls: local=%d cloud=%v", e.localCalls, e.cloudBDHomes)
	}
	// A recipient found on the laptop also keeps the local path.
	e.localIssue = issueJSON(bd.Issue{ID: "at-local1", Status: "open"})
	if handled, err := e.send("at-local1", "at-me1"); handled || err != nil {
		t.Errorf("local initiative: got (%t, %v), want (false, nil)", handled, err)
	}
	if len(e.cloudBDHomes) != 0 {
		t.Errorf("cloud touched for a local initiative: %v", e.cloudBDHomes)
	}
}

func TestCloudSendHostedHomeSkips(t *testing.T) {
	e := newCloudSendEnv(t, true)
	meta := filepath.Join(e.ctx.Home, ".beads")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "metadata.json"),
		[]byte(`{"dolt_mode":"server","dolt_server_host":"db.example.com"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if handled, err := e.send(testCloudInit, "at-me1"); handled || err != nil {
		t.Fatalf("got (%t, %v), want (false, nil)", handled, err)
	}
	if e.localCalls != 0 || len(e.cloudBDHomes) != 0 {
		t.Errorf("hosted home made bd calls")
	}
}

func TestCloudSendNotInCloudFallsThrough(t *testing.T) {
	e := newCloudSendEnv(t, true)
	e.cloudInit = "[]"
	if handled, err := e.send(testCloudInit, "at-me1"); handled || err != nil {
		t.Fatalf("got (%t, %v), want (false, nil)", handled, err)
	}
	if e.commentCalls != 0 {
		t.Error("commented although the initiative is not in the cloud")
	}
}

func TestCloudSendClosedOrNoRunRefuses(t *testing.T) {
	cases := []struct {
		name, init, want string
	}{
		{"closed", issueJSON(bd.Issue{ID: testCloudInit, Status: "closed", Description: "where: run:" + testRunBead}), "is closed; nothing sent"},
		{"no run", issueJSON(bd.Issue{ID: testCloudInit, Status: "open", Description: "where: host:box"}), "has no run yet (where: host:box); nothing sent"},
		{"no where", issueJSON(bd.Issue{ID: testCloudInit, Status: "open"}), "has no run yet (where: none); nothing sent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newCloudSendEnv(t, true)
			e.cloudInit = tc.init
			handled, err := e.send(testCloudInit, "at-me1")
			if !handled || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got (%t, %v), want handled with %q", handled, err, tc.want)
			}
			if e.commentCalls != 0 {
				t.Error("commented on a refused send")
			}
		})
	}
}

func TestCloudSendMissingDispatcherBeadsIsUsageError(t *testing.T) {
	e := newCloudSendEnv(t, true)
	cfg := "cloud_home = \"" + e.cloudHome + "\"\n"
	if err := os.WriteFile(filepath.Join(e.ctx.Home, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	handled, err := e.send(testCloudInit, "at-me1")
	if !handled || err == nil || !strings.Contains(err.Error(), "dispatcher_beads") {
		t.Fatalf("got (%t, %v), want handled usage error naming dispatcher_beads", handled, err)
	}
	var ue *cli.UsageError
	if !errors.As(err, &ue) {
		t.Errorf("error %T is not a usage error", err)
	}
}

func TestCloudSendRunBeadMetaInitiativeMismatchRefuses(t *testing.T) {
	e := newCloudSendEnv(t, true)
	e.runBeadJSON = issueJSON(bd.Issue{ID: testRunBead, Status: "open", Description: "meta-initiative: at-other\n---\nx"})
	handled, err := e.send(testCloudInit, "at-me1")
	if !handled || err == nil || !strings.Contains(err.Error(), "belongs to initiative") {
		t.Fatalf("got (%t, %v), want mismatch refusal", handled, err)
	}
	e.runBeadJSON = issueJSON(bd.Issue{ID: testRunBead, Status: "closed", Description: "meta-initiative: " + testCloudInit})
	handled, err = e.send(testCloudInit, "at-me1")
	if !handled || err == nil || !strings.Contains(err.Error(), "is closed; nothing sent") {
		t.Fatalf("got (%t, %v), want closed-run refusal", handled, err)
	}
	if e.commentCalls != 0 {
		t.Error("commented on a refused send")
	}
}

func TestCloudSendCommentArgsEnvAuthorAndText(t *testing.T) {
	e := newCloudSendEnv(t, true)
	handled, err := e.send(testCloudInit, "at-me1")
	if !handled || err != nil {
		t.Fatalf("got (%t, %v), want (true, nil)", handled, err)
	}
	wantArgs := []string{e.dispatcherDir, "initiative:at-me1", testRunBead}
	if strings.Join(e.commentArgs, "|") != strings.Join(wantArgs, "|") {
		t.Errorf("comment args = %v, want %v", e.commentArgs, wantArgs)
	}
	key := laptopMailKey(hostForTest(e), "at-me1", []byte("hello cloud"))
	if want := cloudCommentText("at-me1", "hello cloud", key); e.commentText != want {
		t.Errorf("comment text = %q, want %q", e.commentText, want)
	}
	if !strings.Contains(e.sqlQuery, "issue_id='"+testRunBead+"'") || !strings.Contains(e.sqlQuery, "actor='initiative:at-me1'") {
		t.Errorf("journal query = %q", e.sqlQuery)
	}
	if !strings.Contains(e.stdout.String(), "sent to cloud initiative "+testCloudInit+" via run bead "+testRunBead) {
		t.Errorf("stdout = %q", e.stdout.String())
	}
	for _, want := range []string{"BD_EVENTS_JOURNAL=1", "BEADS_DOLT_SERVER_TLS=1"} {
		found := false
		for _, v := range runBeadEnv {
			found = found || v == want
		}
		if !found {
			t.Errorf("runBeadEnv lacks %s (the default comment seam sends with it)", want)
		}
	}
}

func TestCloudSendNoJournalRowErrors(t *testing.T) {
	e := newCloudSendEnv(t, true)
	for _, empty := range []string{"[]", "(0 rows)\n"} {
		e = newCloudSendEnv(t, true)
		e.sqlOut = empty
		handled, err := e.send(testCloudInit, "at-me1")
		if empty == "[]" && (!handled || err == nil || !strings.Contains(err.Error(), "without an events-journal row")) {
			t.Fatalf("got (%t, %v), want no-row error", handled, err)
		}
		if empty != "[]" && (!handled || err != nil || !strings.Contains(e.stderr.String(), "warning")) {
			t.Fatalf("non-JSON output: got (%t, %v), want warning", handled, err)
		}
	}
	if !strings.Contains(e.sqlQuery, "seq > 42") {
		t.Errorf("query does not bound by the head: %q", e.sqlQuery)
	}
	// A failed head read only warns.
	e = newCloudSendEnv(t, true)
	e.headErr = errors.New("nohead")
	handled, err := e.send(testCloudInit, "at-me1")
	if !handled || err != nil || !strings.Contains(e.stderr.String(), "head could not be read") || e.commentCalls != 1 {
		t.Fatalf("head failure: got (%t, %v, stderr %q)", handled, err, e.stderr.String())
	}
	// A failed query leaves the comment in place and only warns.
	e = newCloudSendEnv(t, true)
	e.sqlErr = errors.New("boom")
	handled, err = e.send(testCloudInit, "at-me1")
	if !handled || err != nil || !strings.Contains(e.stderr.String(), "warning") {
		t.Fatalf("got (%t, %v, stderr %q), want warning and nil", handled, err, e.stderr.String())
	}
}

func TestCloudSendRetryWithSameKeyIsNoop(t *testing.T) {
	e := newCloudSendEnv(t, true)
	key := laptopMailKey(hostForTest(e), "at-me1", []byte("hello cloud"))
	raw, _ := json.Marshal([]map[string]string{{"text": cloudCommentText("at-me1", "hello cloud", key)}})
	e.comments = string(raw)
	handled, err := e.send(testCloudInit, "at-me1")
	if !handled || err != nil {
		t.Fatalf("got (%t, %v), want (true, nil)", handled, err)
	}
	if e.commentCalls != 0 {
		t.Error("re-sent a comment that already carries the mail-key")
	}
	if !strings.Contains(e.stdout.String(), "already delivered") {
		t.Errorf("stdout = %q", e.stdout.String())
	}
}

func TestCloudSendStewardSenderRepliesToLocalSteward(t *testing.T) {
	e := newCloudSendEnv(t, true)
	if handled, err := e.send(testCloudInit, StewardHandle); !handled || err != nil {
		t.Fatalf("got (%t, %v)", handled, err)
	}
	if !strings.Contains(e.commentText, "Reply with: ateam mail send "+LocalStewardHandle+" --file") {
		t.Errorf("comment text = %q", e.commentText)
	}
	if e.commentArgs[1] != "laptop:steward" {
		t.Errorf("author = %q, want laptop:steward", e.commentArgs[1])
	}
}

// hostForTest returns the host identity the implementation keys messages on.
func hostForTest(e *cloudSendEnv) string { return workspace.Host(e.ctx.Home) }
