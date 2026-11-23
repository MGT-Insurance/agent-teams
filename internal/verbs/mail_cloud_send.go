package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// cloudCommentFunc adds the comment in file to the run bead runBead in the
// Dispatcher's beads database dir, as actor, with the events journal on.
type cloudCommentFunc func(dir, actor, runBead, file string) error

// cloudSQLFunc runs one read-only query against the Dispatcher's beads
// database dir and returns its output.
type cloudSQLFunc func(dir, query string) (string, error)

// Seams for the laptop-to-cloud send. sendKong is frozen by the contract, so
// they are package variables; tests replace them and restore on cleanup.
var (
	cloudSendBD      cloudBDFunc      = defaultCloudBD
	cloudSendComment cloudCommentFunc = defaultCloudComment
	cloudSendSQL     cloudSQLFunc     = defaultCloudSQL
)

// mailKeyPrefix starts the last line of a laptop-to-cloud comment.
const mailKeyPrefix = "mail-key: "

// authorUnsafe matches the characters of an actor name that are not safe to
// interpolate into the journal query.
var authorUnsafe = regexp.MustCompile(`[^A-Za-z0-9._:@+-]`)

// journalSeqDigit matches a sequence number in `bd sql` output. The column
// header "seq" holds no digit, so a match means a row.
var journalSeqDigit = regexp.MustCompile(`[0-9]`)

// runBeadMetaInitiative returns the "meta-initiative:" value from a run bead
// description header (the lines before the "---" separator).
func runBeadMetaInitiative(desc string) string {
	for _, line := range strings.Split(desc, "\n") {
		line = strings.TrimSpace(line)
		if line == "---" {
			break
		}
		if v, ok := strings.CutPrefix(line, "meta-initiative:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// commentCarriesKey reports whether any comment in the `bd comments --json`
// output ends with the line "mail-key: <key>".
func commentCarriesKey(out, key string) bool {
	var comments []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		return false
	}
	want := mailKeyPrefix + key
	for _, c := range comments {
		lines := strings.Split(strings.TrimRight(c.Text, "\r\n "), "\n")
		if strings.TrimSpace(lines[len(lines)-1]) == want {
			return true
		}
	}
	return false
}

func defaultCloudComment(dir, actor, runBead, file string) error {
	cmd := exec.Command("bd", "-C", dir, "--actor", actor, "comment", runBead, "--file", file)
	cmd.Env = append(os.Environ(), runBeadEnv...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func defaultCloudSQL(dir, query string) (string, error) {
	cmd := exec.Command("bd", "-C", dir, "sql", query)
	cmd.Env = append(os.Environ(), runBeadEnv...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// trySendToCloud is the laptop-to-cloud hook in sendKong.Run. It reports
// handled=true when it delivered (or refused) the message itself, so Run
// returns err. When the recipient is not a cloud initiative, or no cloud is
// configured, it returns handled=false and the existing path runs unchanged.
// It writes the message as a comment on the recipient's run bead in the
// Dispatcher's database; Dispatcher resumes the parked run within about 60s.
func (c *sendKong) trySendToCloud(ctx *cli.Context, sender string) (handled bool, err error) {
	if c.RecipientID == StewardHandle || c.RecipientID == LocalStewardHandle {
		return false, nil
	}
	if _, hosted := bd.HostedServer(ctx.Home); hosted {
		return false, nil
	}
	cloudHome, ok, err := bridgeCloudHome(ctx.Home)
	if err != nil || !ok {
		return false, nil
	}

	// A recipient that exists locally keeps the local path.
	if _, err := bd.ShowIssue(ctx.BD, c.RecipientID); err == nil || !bd.IsNotFound(err) {
		return false, nil
	}

	newBD := cloudSendBD
	issue, err := bd.ShowIssue(newBD(cloudHome), c.RecipientID)
	if err != nil {
		if bd.IsNotFound(err) {
			return false, nil
		}
		return true, fmt.Errorf("ateam send: read %s in cloud_home %s: %w; nothing sent", c.RecipientID, cloudHome, err)
	}
	if issue.Status == "closed" {
		return true, fmt.Errorf("ateam send: cloud initiative %s is closed; nothing sent", c.RecipientID)
	}
	where := initiative.Of(issue).Where
	if !strings.HasPrefix(where, whereRunPrefix) {
		shown := where
		if shown == "" {
			shown = "none"
		}
		return true, fmt.Errorf("ateam send: cloud initiative %s has no run yet (where: %s); nothing sent", c.RecipientID, shown)
	}
	runBead := strings.TrimPrefix(where, whereRunPrefix)
	if !runBeadIDPattern.MatchString(runBead) {
		return true, fmt.Errorf("ateam send: cloud initiative %s has an invalid run bead %q; nothing sent", c.RecipientID, runBead)
	}

	dispatcherBeads, ok, err := workspaceconfig.DispatcherBeads(ctx.Home)
	if err != nil {
		return true, fmt.Errorf("ateam send: %w", err)
	}
	if !ok {
		return true, cli.Usagef("ateam send: dispatcher_beads is not set in %s/%s; it is needed to reach cloud initiative %s", ctx.Home, workspaceconfig.FileName, c.RecipientID)
	}
	dispatcher := newBD(dispatcherBeads)
	run, err := bd.ShowIssue(dispatcher, runBead)
	if err != nil {
		return true, fmt.Errorf("ateam send: read run bead %s in %s: %w; nothing sent", runBead, dispatcherBeads, err)
	}
	switch run.Status {
	case "closed", "done", "failed":
		return true, fmt.Errorf("ateam send: run %s is %s; nothing sent", runBead, run.Status)
	}
	if meta := runBeadMetaInitiative(run.Description); meta != c.RecipientID {
		return true, fmt.Errorf("ateam send: run %s belongs to initiative %q, not %s; nothing sent", runBead, meta, c.RecipientID)
	}

	body, err := os.ReadFile(c.File)
	if err != nil {
		return true, fmt.Errorf("ateam send: read %s: %w", c.File, err)
	}
	if len(body) == 0 {
		return true, fmt.Errorf("ateam send: %s is empty; nothing sent", c.File)
	}

	host := workspace.Host(ctx.Home)
	if host == "" {
		host, _ = os.Hostname()
	}
	key := laptopMailKey(host, sender, body)
	if out, err := dispatcher.Run("comments", runBead, "--json"); err == nil && commentCarriesKey(out, key) {
		fmt.Fprintf(ctx.Stdout, "already delivered to cloud initiative %s via run bead %s\n", c.RecipientID, runBead)
		return true, nil
	}

	author := authorUnsafe.ReplaceAllString(cloudCommentAuthor(sender), "-")
	tmp, err := os.CreateTemp("", "ateam-cloud-send-*.txt")
	if err != nil {
		return true, fmt.Errorf("ateam send: create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(cloudCommentText(sender, string(body), key)); err != nil {
		tmp.Close()
		return true, fmt.Errorf("ateam send: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return true, fmt.Errorf("ateam send: write temp file: %w", err)
	}
	if err := cloudSendComment(dispatcherBeads, author, runBead, tmp.Name()); err != nil {
		return true, fmt.Errorf("ateam send: comment on run bead %s in %s: %w; nothing sent", runBead, dispatcherBeads, err)
	}

	query := fmt.Sprintf("select seq from bd_events_journal where issue_id='%s' and op='comment' and actor='%s' order by seq desc limit 1", runBead, author)
	out, qerr := cloudSendSQL(dispatcherBeads, query)
	if qerr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam send: warning: comment written to run bead %s, but its events-journal check failed: %v\n", runBead, qerr)
		return true, nil
	}
	if !journalSeqDigit.MatchString(out) {
		return true, fmt.Errorf("ateam send: comment written to run bead %s without an events-journal row; Dispatcher will not deliver it", runBead)
	}
	fmt.Fprintf(ctx.Stdout, "sent to cloud initiative %s via run bead %s; Dispatcher delivers within about 60s\n", c.RecipientID, runBead)
	return true, nil
}
