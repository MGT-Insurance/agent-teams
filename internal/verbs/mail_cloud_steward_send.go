package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// cloudStewardCreateFunc creates a mail bead assigned to the cloud steward in
// the Dispatcher's beads database dir and returns its id. bodyFile holds the
// bead description; key is the mail key carried as a label.
type cloudStewardCreateFunc func(dir, actor, title, key, bodyFile string) (string, error)

// cloudStewardCreate is the seam for the cloud-steward mail create. Tests
// replace it and restore on cleanup.
var cloudStewardCreate cloudStewardCreateFunc = defaultCloudStewardCreate

// cloudStewardCreateArgs returns the bd argv for the cloud-steward mail bead.
// The bead is a bd task labeled type:mail: the Dispatcher database has no
// custom bd types, and Dispatcher itself records its type as a type:<name>
// label on a task. The create row in the events journal wakes the steward
// through the agent:steward assignee.
func cloudStewardCreateArgs(dir, actor, title, key, bodyFile string) []string {
	return []string{
		"-C", dir,
		"--actor", actor,
		"create",
		"--type=task",
		"--labels", "type:mail," + mailKeyLabelPrefix + key,
		"--assignee=" + cloudStewardAssignee,
		"--title", title,
		"--body-file", bodyFile,
		"--json",
	}
}

func defaultCloudStewardCreate(dir, actor, title, key, bodyFile string) (string, error) {
	cmd := exec.Command("bd", cloudStewardCreateArgs(dir, actor, title, key, bodyFile)...)
	cmd.Env = append(os.Environ(), runBeadEnv...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	// bd create --json prints one object; tolerate a one-element array.
	var issue bd.Issue
	if err := json.Unmarshal(out, &issue); err != nil {
		var arr []bd.Issue
		if aerr := json.Unmarshal(out, &arr); aerr != nil || len(arr) != 1 {
			return "", fmt.Errorf("parse bd create output: %w", err)
		}
		issue = arr[0]
	}
	if issue.ID == "" {
		return "", errors.New("bd create returned no id")
	}
	return issue.ID, nil
}

// recentMailWithKey returns the id of an issue in the `bd list --json` output
// created within cloudDedupWindow of now. A created_at that does not parse
// counts as recent, so an unreadable timestamp never causes a duplicate.
func recentMailWithKey(out string, now time.Time) (string, bool) {
	var issues []bd.Issue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return "", false
	}
	for _, iss := range issues {
		if at, err := time.Parse(time.RFC3339, iss.CreatedAt); err == nil && now.Sub(at) > cloudDedupWindow {
			continue
		}
		return iss.ID, true
	}
	return "", false
}

// sendToCloudSteward is the laptop-to-cloud-steward hook of send. It creates a
// mail bead assigned to agent:steward in the Dispatcher's beads database;
// Dispatcher wakes the cloud steward for it within about 60s. Every refusal
// returns an error and writes nothing.
func (c *sendKong) sendToCloudSteward(ctx *cli.Context, sender string) error {
	if _, hosted := bd.HostedServer(ctx.Home); hosted {
		return errors.New("ateam send: cloud-steward is reached from the laptop; this workspace is hosted; nothing sent")
	}
	if sender != StewardHandle {
		return errors.New("ateam send: only the laptop steward can mail cloud-steward; mail your steward instead; nothing sent")
	}
	if _, ok, err := bridgeCloudHome(ctx.Home); err != nil {
		return fmt.Errorf("ateam send: %w; nothing sent", err)
	} else if !ok {
		return cli.Usagef("ateam send: cloud_home is not set in %s/%s; the cloud steward's reply comes back over the bridge", ctx.Home, workspaceconfig.FileName)
	}
	dispatcherBeads, ok, err := workspaceconfig.DispatcherBeads(ctx.Home)
	if err != nil {
		return fmt.Errorf("ateam send: %w", err)
	}
	if !ok {
		return cli.Usagef("ateam send: dispatcher_beads is not set in %s/%s; it is needed to reach %s", ctx.Home, workspaceconfig.FileName, CloudStewardHandle)
	}
	body, err := os.ReadFile(c.File)
	if err != nil {
		return fmt.Errorf("ateam send: read %s: %w", c.File, err)
	}
	if len(body) == 0 {
		return fmt.Errorf("ateam send: %s is empty; nothing sent", c.File)
	}

	host := workspace.Host(ctx.Home)
	if host == "" {
		host, _ = os.Hostname()
	}
	key := laptopMailKey(host, sender, body)
	dispatcher := cloudSendBD(dispatcherBeads)
	if out, err := dispatcher.Run("list", "--type=task", "--label=type:mail", "--label="+mailKeyLabelPrefix+key,
		"--assignee="+cloudStewardAssignee, "--all", "--json"); err == nil {
		if id, hit := recentMailWithKey(out, cloudSendNow()); hit {
			fmt.Fprintf(ctx.Stdout, "already delivered to %s as %s\n", CloudStewardHandle, id)
			return nil
		}
	}

	author := authorUnsafe.ReplaceAllString(cloudCommentAuthor(sender), "-")
	tmp, err := os.CreateTemp("", "ateam-cloud-steward-*.txt")
	if err != nil {
		return fmt.Errorf("ateam send: create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(cloudStewardMailBody(LocalStewardHandle, string(body), key)); err != nil {
		tmp.Close()
		return fmt.Errorf("ateam send: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("ateam send: write temp file: %w", err)
	}

	var (
		head    int64
		headErr error
	)
	if headOut, err := cloudSendSQL(dispatcherBeads, journalHeadQuery); err != nil {
		headErr = err
	} else {
		head, headErr = journalHead(headOut)
	}
	id, err := cloudStewardCreate(dispatcherBeads, author, cloudStewardMailTitle(LocalStewardHandle), key, tmp.Name())
	if err != nil {
		return fmt.Errorf("ateam send: create mail bead in %s: %w; nothing sent", dispatcherBeads, err)
	}
	if !runBeadIDPattern.MatchString(id) {
		return fmt.Errorf("ateam send: bd create returned an unexpected id %q; check %s", id, dispatcherBeads)
	}

	if headErr != nil {
		fmt.Fprintf(ctx.Stderr, "ateam send: warning: mail bead %s created, but the events-journal head could not be read: %v\n", id, headErr)
	} else {
		query := fmt.Sprintf("select seq from bd_events_journal where seq > %d and issue_id='%s' and op='create' limit 1", head, id)
		out, qerr := cloudSendSQL(dispatcherBeads, query)
		var rows []map[string]any
		if qerr == nil {
			rows, qerr = sqlRows(out)
		}
		switch {
		case qerr != nil:
			fmt.Fprintf(ctx.Stderr, "ateam send: warning: mail bead %s created, but its events-journal check failed: %v\n", id, qerr)
		case len(rows) == 0:
			return fmt.Errorf("ateam send: created %s without an events-journal row; Dispatcher will not wake the steward", id)
		}
	}
	fmt.Fprintf(ctx.Stdout, "message_id: %s\n", id)
	fmt.Fprintf(ctx.Stdout, "sent to %s; Dispatcher wakes it within about 60s\n", CloudStewardHandle)
	return nil
}
