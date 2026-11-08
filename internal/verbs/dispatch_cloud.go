package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

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

// hostedTLSEnv is the env every bd call against the hosted Dolt server needs:
// the server rejects non-TLS connections, and bd has no persisted setting for
// it (--server-tls on init is not persisted).
const hostedTLSEnv = "BEADS_DOLT_SERVER_TLS=1"

// runBeadEnv is the env for the calls against the Dispatcher's database. The
// Dispatcher's autostart reads only the events journal, which bd writes only
// when it is on.
var runBeadEnv = []string{hostedTLSEnv, "BD_EVENTS_JOURNAL=1"}

func defaultCloudBD(home string) cli.BDRunner { return bd.NewClientWithEnv(home, hostedTLSEnv) }

// runBeadIDPattern is the shape of a bd issue id. The id is interpolated into
// the journal query, so anything else is refused.
var runBeadIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// defaultFileRunBead runs
//
//	bd -C <dir> create --title <title> --type task --priority 2
//	  --labels type:ateam-dri --actor <actor> --body-file <tmp> --silent
//
// with BEADS_DOLT_SERVER_TLS=1 (the way the Dispatcher's own bd wrapper does)
// and BD_EVENTS_JOURNAL=1. Dispatcher autostart reads only bd_events_journal,
// and bd writes a journal row only when the journal is on, so after the create
// it checks that a create row exists for the new id and fails loudly if not.
func defaultFileRunBead(dir, actor, title, body string) (string, error) {
	return createJournaledBead(dir, append(os.Environ(), runBeadEnv...), "ateam-run-bead-*.txt",
		[]string{
			"--title", title,
			"--type", "task",
			"--priority", "2",
			"--labels", "type:ateam-dri",
			"--actor", actor,
		}, body, "created without an events-journal row; closed by ateam dispatch")
}

// createJournaledBead runs `bd -C dir create <createArgs> --body-file <tmp>
// --silent` with env (the full process environment, which must carry
// runBeadEnv), checks the returned id, and verifies the events-journal create
// row. With no row it closes the orphan bead with orphanReason. A failure after
// the create is a *postCreateError naming the bead. Both `dispatch --cloud` and
// `ui-test file` file their beads through it.
func createJournaledBead(dir string, env []string, tmpPattern string, createArgs []string, body, orphanReason string) (string, error) {
	f, err := os.CreateTemp("", tmpPattern)
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
	args := append([]string{"-C", dir, "create"}, createArgs...)
	args = append(args, "--body-file", f.Name(), "--silent")
	cmd := exec.Command("bd", args...)
	cmd.Env = env
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
	if !runBeadIDPattern.MatchString(id) {
		return "", fmt.Errorf("bd create returned an unexpected id %q", id)
	}
	if perr := verifyJournalRow(dir, env, id); perr != nil {
		if perr.Kind == postCreateNoRow {
			perr.CloseErr = closeRunBead(dir, env, id, orphanReason)
			perr.Closed = perr.CloseErr == nil
		}
		return "", perr
	}
	return id, nil
}

// closeRunBead closes the run bead id in dir with the same env as create.
func closeRunBead(dir string, env []string, id, reason string) error {
	cmd := exec.Command("bd", "-C", dir, "close", id, "--reason="+reason)
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// postCreateKind says which check failed after bd create succeeded.
type postCreateKind int

const (
	// postCreateNoRow means the journal query ran and found no create row.
	postCreateNoRow postCreateKind = iota
	// postCreateCheckFailed means the journal query itself failed, so whether
	// the row exists is unknown.
	postCreateCheckFailed
)

// postCreateError reports a failure after the run bead ID was created. Unlike a
// create failure, the bead exists in the Dispatcher's database.
type postCreateError struct {
	ID, Dir  string
	Kind     postCreateKind
	Cause    error  // the query failure, for postCreateCheckFailed
	Closed   bool   // postCreateNoRow: the orphan run bead was closed
	CloseErr error  // postCreateNoRow: why closing it failed
	Msg      string // the underlying description
}

func (e *postCreateError) Error() string { return e.Msg }
func (e *postCreateError) Unwrap() error { return e.Cause }

// journalQuery is the bd sql query that looks up the create row for id.
func journalQuery(id string) string {
	return fmt.Sprintf("select issue_id from bd_events_journal where issue_id = '%s' and op = 'create' limit 1", id)
}

// verifyJournalRow returns a *postCreateError unless bd_events_journal in dir
// holds a create row for id. The query asks for the row's issue_id rather than
// a count, so the check does not depend on bd's table output format.
func verifyJournalRow(dir string, env []string, id string) *postCreateError {
	cmd := exec.Command("bd", "-C", dir, "sql", journalQuery(id))
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return &postCreateError{ID: id, Dir: dir, Kind: postCreateCheckFailed, Cause: err,
			Msg: fmt.Sprintf("checking its events-journal row failed: %v: %s", err, strings.TrimSpace(stderr.String()))}
	}
	if !strings.Contains(string(out), id) {
		return &postCreateError{ID: id, Dir: dir, Kind: postCreateNoRow,
			Msg: "no create row in bd_events_journal"}
	}
	return nil
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
	const ghPrefix = "https://github.com/"
	rest := strings.TrimPrefix(repoURL, ghPrefix)
	owner, path, _ := strings.Cut(rest, "/")
	if !strings.HasPrefix(repoURL, ghPrefix) || !strings.EqualFold(owner, cloudOwner) {
		return cli.Usagef("dispatch --cloud: origin %s is not owned by %s", repoURL, cloudOwner)
	}
	repoURL = ghPrefix + cloudOwner + "/" + path
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
	// Fail-soft: registering the initiative does not need the setup.
	setup := c.hostedSetup
	if setup == nil {
		setup = ensureHostedHomeSetupSoft
	}
	setup(&cli.Context{Home: settings.home, BD: cloud, Stdout: ctx.Stdout, Stderr: ctx.Stderr}, false)

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
		var pce *postCreateError
		if errors.As(err, &pce) {
			return c.handlePostCreate(ctx, cloud, settings, issue.ID, pce)
		}
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

// handlePostCreate handles a failure after the run bead was created. With no
// journal row the Dispatcher never sees the bead, so the initiative is closed.
// When the check itself failed the row may exist, so nothing is closed and the
// run bead is recorded on the initiative.
func (c *dispatchKong) handlePostCreate(ctx *cli.Context, cloud cli.BDRunner, settings cloudSettings, initID string, pce *postCreateError) error {
	if pce.Kind == postCreateCheckFailed {
		if err := noteOn(cloud, initID, fmt.Sprintf("cloud run bead %s was filed; its events-journal check failed, so the dispatch may have succeeded", pce.ID)); err != nil {
			fmt.Fprintf(ctx.Stderr, "dispatch: warning: could not note run bead on %s (fail-soft): %v\n", initID, err)
		}
		return fmt.Errorf("dispatch: run bead %s was filed in %s for initiative %s, but %s; the dispatch may have succeeded and the initiative is still open. Check with: BEADS_DOLT_SERVER_TLS=1 bd -C %s sql \"%s\" (a row means the Dispatcher will start it)",
			pce.ID, settings.dispatcherBeads, initID, pce.Msg, settings.dispatcherBeads, journalQuery(pce.ID))
	}
	advice := fmt.Sprintf("check that dispatcher_beads (%s) is bound to the main beads_<user> database and that events-journal is on, then re-run dispatch", settings.dispatcherBeads)
	var reason, runBeadNote string
	if pce.Closed {
		reason = fmt.Sprintf("cloud run bead %s created without an events-journal row; closed it", pce.ID)
	} else {
		reason = fmt.Sprintf("cloud run bead %s created without an events-journal row; could not close it", pce.ID)
		runBeadNote = fmt.Sprintf("; also failed to close run bead %s (close it manually with bd -C %s close %s): %v", pce.ID, settings.dispatcherBeads, pce.ID, pce.CloseErr)
	}
	if _, closeErr := cloud.Run("close", initID, "--reason="+reason); closeErr != nil {
		return fmt.Errorf("dispatch: %s%s; also failed to close initiative %s (close it manually): %v; %s", reason, runBeadNote, initID, closeErr, advice)
	}
	return fmt.Errorf("dispatch: %s%s; %s", reason, runBeadNote, advice)
}

// kebabSlugPattern is the shape of a --slug accepted in register-only mode.
var kebabSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// cutTitle returns s cut to at most max bytes, backing up to a rune boundary
// so the result stays valid UTF-8.
func cutTitle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// repoNamePattern is the shape of a GitHub repository name.
var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// parseCloudRepoURL normalizes a --repo-url: https://github.com/<owner>/<name>
// with an optional trailing "/" and ".git". The owner must equal cloudOwner
// case-insensitively and is rewritten to cloudOwner. It returns the canonical
// URL and the repository name.
func parseCloudRepoURL(raw string) (url, name string, err error) {
	const ghPrefix = "https://github.com/"
	bad := cli.Usagef("dispatch: --repo-url %q must look like %s%s/<name>", raw, ghPrefix, cloudOwner)
	rest, ok := strings.CutPrefix(raw, ghPrefix)
	if !ok {
		return "", "", bad
	}
	rest = strings.TrimSuffix(rest, "/")
	rest = strings.TrimSuffix(rest, ".git")
	owner, name, found := strings.Cut(rest, "/")
	if !found || !strings.EqualFold(owner, cloudOwner) {
		return "", "", bad
	}
	if !repoNamePattern.MatchString(name) || name == "." || name == ".." {
		return "", "", bad
	}
	return ghPrefix + cloudOwner + "/" + name, name, nil
}

// registerOnlyResult is the one JSON line --register-only prints.
type registerOnlyResult struct {
	Initiative string `json:"initiative"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	Team       string `json:"team"`
	Mode       string `json:"mode"`
}

// resolveRegisterOnlyHome returns the hosted workspace a register-only
// initiative goes to: config cloud_home, else home itself when it is bound to
// a hosted Dolt server.
func resolveRegisterOnlyHome(home string) (string, error) {
	cloudHome, ok, err := workspaceconfig.CloudHome(home)
	if err != nil {
		return "", fmt.Errorf("dispatch --register-only: %w", err)
	}
	if ok {
		return cloudHome, nil
	}
	if _, hosted := bd.HostedServer(home); hosted {
		return home, nil
	}
	return "", cli.Usagef("dispatch --register-only: no hosted workspace: set cloud_home in %s/%s, or point AGENT_TEAMS_HOME at a workspace bound to a hosted Dolt server", home, workspaceconfig.FileName)
}

// runCloudRegisterOnly registers a cloud initiative in the hosted workspace
// and prints one JSON line. It needs no git checkout, no repoconfig opt-in, no
// dispatcher_beads, and no dispatcher_actor. It files no run bead, runs no
// hosted-home setup, and writes no note.
func (c *dispatchKong) runCloudRegisterOnly(ctx *cli.Context) error {
	repoURL, repoName, err := parseCloudRepoURL(c.RepoURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.Problem) == "" {
		return cli.Usagef("dispatch: --problem must not be blank")
	}
	if strings.ContainsAny(c.Problem, "\r\n") {
		return cli.Usagef("dispatch: --problem must be a single line")
	}
	slug := c.Slug
	if slug == "" {
		slug = gitutil.Slugify(c.Problem)
	}
	if slug == "" {
		return cli.Usagef("dispatch: --problem produced an empty slug; provide --slug explicitly")
	}
	if !kebabSlugPattern.MatchString(slug) || gitutil.Slugify(slug) != slug {
		return cli.Usagef("dispatch: --slug %q must be a kebab-case slug (ASCII lowercase letters, digits, and single hyphens, at most 50 characters)", slug)
	}
	home, err := resolveRegisterOnlyHome(ctx.Home)
	if err != nil {
		return err
	}

	team := gitutil.Slugify(repoName) + "-" + slug
	plan, err := initiative.New(initiative.Fields{
		Problem: c.Problem,
		Repo:    repoURL,
		Branch:  slug,
		Team:    team,
		Mode:    "cloud",
		Runtime: "claude",
	})
	if err != nil {
		return cli.Usagef("dispatch: %v", err)
	}

	shortTitle := cutTitle(c.Problem, 72)
	tmp, err := os.CreateTemp("", "ateam-dispatch-*.txt")
	if err != nil {
		return fmt.Errorf("dispatch: create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(plan.Description); err != nil {
		tmp.Close()
		return fmt.Errorf("dispatch: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("dispatch: write temp file: %w", err)
	}

	newCloudBD := c.cloudBD
	if newCloudBD == nil {
		newCloudBD = defaultCloudBD
	}
	var issue bd.Issue
	if err := newCloudBD(home).RunJSON(&issue, "create",
		"--title="+shortTitle,
		"--type=task",
		"--priority=2",
		"--body-file="+tmp.Name(),
		"--json",
	); err != nil {
		return fmt.Errorf("dispatch: register initiative in %s: %w", home, err)
	}
	if issue.ID == "" {
		return fmt.Errorf("dispatch: bd create in %s returned no id", home)
	}

	line, err := json.Marshal(registerOnlyResult{
		Initiative: issue.ID, Repo: repoURL, Branch: slug, Team: team, Mode: "cloud",
	})
	if err != nil {
		return fmt.Errorf("dispatch: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "%s\n", line)
	return nil
}
