// This file exists because of an open Claude Code bug
// (anthropics/claude-code#78234, #81746): the in-process teammate spawn path
// discards plugin- and built-in-sourced agent definitions when a name is
// passed, silently handing the teammate a generic system prompt. Generating
// a CLI-scope --agents payload from the plugin's own roles/*.md at launch
// time (claude --agents survives that path) is the workaround. See
// plugins/agent-teams/roles/README.md for the full mechanism, the frozen
// payload contract (agent-teams-wf7o.9), and the removal condition.
package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// agentDefinition is one entry in the --agents JSON payload (frozen shape,
// agent-teams-wf7o.9 artifact (2)). No "tools" key: none of the role
// files carry a tools frontmatter key, so there is nothing to allowlist.
type agentDefinition struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model,omitempty"`
}

// resolveRolesDir locates the plugin's roles/ directory per the resolution
// frozen in agent-teams-wf7o.9 artifact (3): prefer $CLAUDE_PLUGIN_ROOT/roles
// when the env var is set and that directory exists; otherwise resolve
// relative to the running ateam binary itself, filepath.Dir(self)/../roles —
// the same self-locating pattern already shipped by runUpdateLocalMainScript
// (kong_converted.go) and block-model-divergence.sh. This must resolve
// correctly whether ateam is running from a dev checkout (bin/ wrapper ->
// exec chain) or the marketplace cache copy
// (~/.claude/plugins/cache/agent-teams/agent-teams/<version>/): both lay
// bin/ and roles/ out as siblings, so both resolution strategies land on the
// same relative shape.
//
// Fails loud (agent-teams-wf7o.9 artifact (4)) rather than returning "": after
// the roles/ move there is no plugin-scope fallback left at all, so silently
// proceeding without --agents would launch a DRI whose teammates are all
// silently generic. The returned error names every path tried.
// errRolesDirUnresolvable marks the ENVIRONMENT failure — no roles/ directory
// could be located at all — as distinct from an install defect found INSIDE a
// roles/ directory that did resolve. Callers that map failures to exit codes
// need the difference: an unresolvable roles dir can happen with a perfectly
// healthy install (wrong cwd, unset $CLAUDE_PLUGIN_ROOT, binary run from a
// scratch path), so it is "the tool could not run"; a malformed or missing
// role file inside a resolved directory IS the install being broken.
var errRolesDirUnresolvable = errors.New("roles directory unresolvable")

func resolveRolesDir() (string, error) {
	var tried []string

	if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		dir := filepath.Join(root, "roles")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
		tried = append(tried, dir+" (from $CLAUDE_PLUGIN_ROOT)")
	}

	self, err := os.Executable()
	if err != nil {
		tried = append(tried, fmt.Sprintf("<self-binary unresolvable: %v>", err))
		return "", fmt.Errorf("resolve plugin roles dir: no roles/*.md found; tried: %s: %w", strings.Join(tried, "; "), errRolesDirUnresolvable)
	}
	dir := filepath.Join(filepath.Dir(self), "..", "roles")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	tried = append(tried, dir+" (relative to self binary "+self+")")

	return "", fmt.Errorf("resolve plugin roles dir: no roles/*.md found; tried: %s: %w", strings.Join(tried, "; "), errRolesDirUnresolvable)
}

// nonRoleFiles names *.md files directly inside roles/ that are NOT role
// definitions and must be skipped by name before parsing — never by a
// content-shaped heuristic like "lacks frontmatter". agent-teams-wf7o.18:
// roles/README.md is the workaround doc for this very mechanism (see the
// package doc comment above); it deliberately has no frontmatter, but it
// stays in roles/ rather than moving elsewhere so it sits next to what it
// documents. Any OTHER frontmatter-less .md file in this directory is a
// genuinely malformed role and must still fail loud — that is the exact
// failure class this initiative exists to catch, so do not widen this to
// "skip anything without frontmatter" and do not hardcode role names here
// either (this is an exclude-list of non-roles, not an allow-list of roles).
var nonRoleFiles = map[string]bool{
	"README.md": true,
}

// buildAgentsJSON parses every *.md file directly inside dir — except the
// non-role files named in nonRoleFiles — into the --agents JSON payload
// (agent-teams-wf7o.9 artifact (2)) and returns it marshaled. Key order is
// deterministic: encoding/json sorts map[string]... keys when marshaling,
// and the input files are walked in sorted order too, so the argv this
// feeds is stable and diffable across runs.
//
// Fails loud on any problem — an empty/missing dir, a file that isn't
// parseable frontmatter+body, or zero *.md files found — rather than
// returning a partial or empty payload silently.
func buildAgentsJSON(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read roles dir %s: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if nonRoleFiles[e.Name()] {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no role *.md files found in %s", dir)
	}
	sort.Strings(names)

	payload := make(map[string]agentDefinition, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read role file %s: %w", path, err)
		}
		def, err := parseRoleFile(string(data))
		if err != nil {
			return "", fmt.Errorf("parse role file %s: %w", path, err)
		}
		role := strings.TrimSuffix(name, ".md")
		payload["agent-teams-"+role] = def
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal agents payload: %w", err)
	}
	return string(b), nil
}

// parseRoleFile parses one role .md file's raw content into an
// agentDefinition per agent-teams-wf7o.9 artifact (2):
//   - description = frontmatter "description:" value, verbatim (quotes and
//     surrounding whitespace trimmed).
//   - model = frontmatter "model:" value (quotes/whitespace trimmed), OMITTED
//     (left "") when there is no model line or its value is "inherit".
//   - prompt = the file body after the closing frontmatter "---", with
//     leading blank lines stripped.
//
// No "name" key: the payload's JSON object key supplies the name, and
// role/agent names may never contain ':' — see wf7o.9 for why.
func parseRoleFile(content string) (agentDefinition, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return agentDefinition{}, fmt.Errorf("missing opening frontmatter delimiter '---'")
	}

	var description, model string
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
		key, val, ok := strings.Cut(lines[i], ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "description":
			description = val
		case "model":
			model = val
		}
	}
	if closeIdx == -1 {
		return agentDefinition{}, fmt.Errorf("missing closing frontmatter delimiter '---'")
	}
	if description == "" {
		return agentDefinition{}, fmt.Errorf("frontmatter missing a non-empty description")
	}

	bodyLines := lines[closeIdx+1:]
	start := 0
	for start < len(bodyLines) && strings.TrimSpace(bodyLines[start]) == "" {
		start++
	}
	body := strings.Join(bodyLines[start:], "\n")
	if strings.TrimSpace(body) == "" {
		return agentDefinition{}, fmt.Errorf("role body is empty after stripping frontmatter")
	}

	if model == "inherit" {
		model = ""
	}

	return agentDefinition{Description: description, Prompt: body, Model: model}, nil
}

// buildAgentsPayload resolves the roles directory and builds the --agents
// JSON payload from it in one step. Shared by the `ateam agents-json` verb
// (which makes the payload inspectable without launching a session) and
// rawLaunchBGSession (the actual production launch path).
func buildAgentsPayload() (string, error) {
	dir, err := resolveRolesDir()
	if err != nil {
		return "", err
	}
	return buildAgentsJSON(dir)
}

// ── agents-json verb ─────────────────────────────────────────────────────────

// agentsJSONKong is the kong-converted form of `ateam agents-json`. No
// arguments — it prints the --agents JSON payload generated from
// plugins/agent-teams/roles/*.md to stdout. This exists so the payload is
// inspectable without launching a background session: it is how the shell
// tests assert the contract and how a human debugs a bad role definition.
type agentsJSONKong struct {
	// buildPayload is injected so tests can substitute a fixture without
	// touching a package global; nil means "use buildAgentsPayload".
	buildPayload func() (string, error) `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
func (c *agentsJSONKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam agents-json: no context")
	}
	build := c.buildPayload
	if build == nil {
		build = buildAgentsPayload
	}
	payload, err := build()
	if err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, payload)
	return nil
}

// RegisterAgentsJSONKong registers the agents-json verb onto p.
func RegisterAgentsJSONKong(p *cli.Parser) {
	p.AddVerb("agents-json", "Print the --agents JSON payload generated from plugins/agent-teams/roles/*.md.", &agentsJSONKong{})
}
