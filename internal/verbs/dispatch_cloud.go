package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/gitutil"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// cloudOwner is the only GitHub organization --cloud dispatches for: the
// Dispatcher's repo catalogs are per organization.
const cloudOwner = "MGT-Insurance"

// dispatcherActorEnv overrides the dispatcher_actor config key.
const dispatcherActorEnv = "ATEAM_DISPATCHER_ACTOR"

// fileRunBeadFunc files one ateam-dri run bead in the Dispatcher's beads
// database (the bd client directory dir) as actor, and returns its id.
type fileRunBeadFunc func(dir, actor, title, body string) (id string, err error)

// cloudBDFunc returns a bd client bound to a workspace home. Dispatch uses it
// for the hosted workspace that cloud initiatives register in, which is not
// the laptop workspace ctx.BD points at.
type cloudBDFunc func(home string) cli.BDRunner

func defaultCloudBD(home string) cli.BDRunner { return bd.NewClient(home) }

// defaultFileRunBead runs
//
//	bd -C <dir> create --title <title> --type task --priority 2
//	  --labels type:ateam-dri --actor <actor> --body-file <tmp> --silent
//
// with BEADS_DOLT_SERVER_TLS=1, the way the Dispatcher's own bd wrapper does.
func defaultFileRunBead(dir, actor, title, body string) (string, error) {
	f, err := os.CreateTemp("", "ateam-run-bead-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	cmd := exec.Command("bd", "-C", dir, "create",
		"--title", title,
		"--type", "task",
		"--priority", "2",
		"--labels", "type:ateam-dri",
		"--actor", actor,
		"--body-file", f.Name(),
		"--silent")
	cmd.Env = append(os.Environ(), "BEADS_DOLT_SERVER_TLS=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("bd create: %w: %s", err, msg)
		}
		return "", fmt.Errorf("bd create: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("bd create returned no id")
	}
	return id, nil
}

// cloudSettings is the resolved --cloud configuration.
type cloudSettings struct {
	home, dispatcherBeads, actor string
}

// resolveCloudSettings reads cloud_home, dispatcher_beads, and the dispatcher
// actor (env ATEAM_DISPATCHER_ACTOR beats the dispatcher_actor key). A missing
// value is a usage error naming it. It never falls back to BEADS_ACTOR, the
// git user name, or USER.
func resolveCloudSettings(home string) (cloudSettings, error) {
	var s cloudSettings
	required := func(key string, read func(string) (string, bool, error)) (string, error) {
		v, ok, err := read(home)
		if err != nil {
			return "", fmt.Errorf("dispatch --cloud: %w", err)
		}
		if !ok {
			return "", cli.Usagef("dispatch --cloud: %s is not set in %s/%s", key, home, workspaceconfig.FileName)
		}
		return v, nil
	}
	var err error
	if s.home, err = required("cloud_home", workspaceconfig.CloudHome); err != nil {
		return s, err
	}
	if s.dispatcherBeads, err = required("dispatcher_beads", workspaceconfig.DispatcherBeads); err != nil {
		return s, err
	}
	if s.actor = strings.TrimSpace(os.Getenv(dispatcherActorEnv)); s.actor == "" {
		if s.actor, err = required("dispatcher_actor", workspaceconfig.DispatcherActor); err != nil {
			return s, fmt.Errorf("%w (or set $%s)", err, dispatcherActorEnv)
		}
	}
	if strings.HasPrefix(s.actor, "agent:") {
		return s, cli.Usagef("dispatch --cloud: dispatcher actor %q is an agent actor, which the Dispatcher never autostarts; use your own account email", s.actor)
	}
	return s, nil
}

// cloudRunBeadBody composes the run bead description: the header the Dispatcher
// parses, a "---" line, then the problem.
func cloudRunBeadBody(repoURL, base, sha, initiativeID, problem string) string {
	return strings.Join([]string{
		"repo: " + repoURL,
		"branch: " + base,
		"sha: " + sha,
		"type: ateam-dri",
		"initiative: " + initiativeID,
		"meta-initiative: " + initiativeID,
		"start: auto",
		"---",
		problem,
		"",
	}, "\n")
}

// runCloud registers the initiative in the hosted workspace (cloud_home) and
// files a start:auto run bead with the Dispatcher instead of creating a local
// worktree and session. base and slug are the resolved base branch and slug.
func (c *dispatchKong) runCloud(ctx *cli.Context, repoRoot, base, slug string) error {
	settings, err := resolveCloudSettings(ctx.Home)
	if err != nil {
		return err
	}

	repoURL, err := c.git.RemoteURL(repoRoot)
	if err != nil {
		return fmt.Errorf("dispatch --cloud: %w", err)
	}
	if owner := strings.Split(strings.TrimPrefix(repoURL, "https://github.com/"), "/")[0]; owner != cloudOwner {
		return cli.Usagef("dispatch --cloud: origin %s is not owned by %s", repoURL, cloudOwner)
	}
	sha, err := c.git.RemoteHead(repoRoot, base)
	if err != nil {
		return fmt.Errorf("dispatch --cloud: %w", err)
	}

	team := gitutil.Slugify(filepath.Base(repoRoot)) + "-" + slug
	fields := initiative.Fields{
		Problem: c.Problem,
		Repo:    repoURL,
		Branch:  slug,
		Team:    team,
		Mode:    "cloud",
		Runtime: "claude",
	}
	plan, err := initiative.New(fields)
	if err != nil {
		return fmt.Errorf("dispatch: %w", err)
	}
	body := plan.Description
	if c.BodyFile != "" {
		extra, err := os.ReadFile(c.BodyFile)
		if err != nil {
			return cli.Usagef("dispatch: --body-file %q: %v", c.BodyFile, err)
		}
		if len(strings.TrimSpace(string(extra))) > 0 {
			for _, col := range fields.CollisionsIn(string(extra)) {
				fmt.Fprintf(ctx.Stderr,
					"dispatch: warning: --body-file line %d redefines routing field %q (first-wins — this line is IGNORED; the header's value stands): %s\n",
					col.Line, col.Key, col.Text)
			}
			body += "\n" + string(extra)
		}
	}

	newCloudBD := c.cloudBD
	if newCloudBD == nil {
		newCloudBD = defaultCloudBD
	}
	cloud := newCloudBD(settings.home)

	shortTitle := c.Problem
	if len(shortTitle) > 72 {
		shortTitle = shortTitle[:72]
	}
	tmp, err := os.CreateTemp("", "ateam-dispatch-*.txt")
	if err != nil {
		return fmt.Errorf("dispatch: create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return fmt.Errorf("dispatch: write temp file: %w", err)
	}
	tmp.Close()

	var issue bd.Issue
	if err := cloud.RunJSON(&issue, "create",
		"--title="+shortTitle,
		"--type=task",
		"--priority=2",
		"--body-file="+tmp.Name(),
		"--json",
	); err != nil {
		return fmt.Errorf("dispatch: register initiative in %s: %w", settings.home, err)
	}
	if issue.ID == "" {
		return fmt.Errorf("dispatch: bd create in %s returned no id", settings.home)
	}

	fileBead := c.fileRunBead
	if fileBead == nil {
		fileBead = defaultFileRunBead
	}
	runBead, err := fileBead(settings.dispatcherBeads, settings.actor, c.Problem,
		cloudRunBeadBody(repoURL, base, sha, issue.ID, c.Problem))
	if err != nil {
		reason := fmt.Sprintf("cloud run bead not filed: %v", err)
		if _, closeErr := cloud.Run("close", issue.ID, "--reason="+reason); closeErr != nil {
			return fmt.Errorf("dispatch: %s; also failed to close initiative %s (close it manually): %v", reason, issue.ID, closeErr)
		}
		return fmt.Errorf("dispatch: %s", reason)
	}

	if err := noteOn(cloud, issue.ID, fmt.Sprintf("cloud run bead %s filed in beads_eric_lloyd", runBead)); err != nil {
		fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not note run bead on %s (fail-soft): %v\n", issue.ID, err)
	}

	if c.IDOnly {
		fmt.Fprintln(ctx.Stdout, issue.ID)
		return nil
	}
	fmt.Fprintf(ctx.Stdout, "dispatched %s to Dispatcher run bead %s (repo %s, base %s @ %s)\n",
		issue.ID, runBead, repoURL, base, sha[:7])
	return nil
}

// noteOn adds text as a note on id through runner.
func noteOn(runner cli.BDRunner, id, text string) error {
	f, err := os.CreateTemp("", "ateam-note-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(text + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_, err = runner.Run("note", id, "--file="+f.Name())
	return err
}
