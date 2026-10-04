package promptsync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Surface struct {
	Path string
	Kind Kind
}

func Discover(root string) ([]Surface, error) {
	var surfaces []Surface
	roleRoot := filepath.Join(root, "plugins", "agent-teams", "roles")
	codexRoot := filepath.Join(root, "internal", "verbs", "codex_agents")
	for _, required := range []string{roleRoot, codexRoot, filepath.Join(root, "plugins", "agent-teams", "skills"), filepath.Join(root, "plugins", "agent-teams-codex", "skills")} {
		info, err := os.Stat(required)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("coverage root missing or not a directory: %s", displayPath(root, required))
		}
	}

	roles, err := os.ReadDir(roleRoot)
	if err != nil {
		return nil, err
	}
	for _, item := range roles {
		if !item.IsDir() && strings.HasSuffix(item.Name(), ".md") && item.Name() != "README.md" {
			surfaces = append(surfaces, Surface{Path: relativeSlash(root, filepath.Join(roleRoot, item.Name())), Kind: KindRole})
		}
	}
	codex, err := os.ReadDir(codexRoot)
	if err != nil {
		return nil, err
	}
	for _, item := range codex {
		if !item.IsDir() && strings.HasSuffix(item.Name(), ".toml") {
			surfaces = append(surfaces, Surface{Path: relativeSlash(root, filepath.Join(codexRoot, item.Name())), Kind: KindRole})
		}
	}

	for _, skillRoot := range []string{filepath.Join(root, "plugins", "agent-teams", "skills"), filepath.Join(root, "plugins", "agent-teams-codex", "skills")} {
		err := filepath.WalkDir(skillRoot, func(path string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if item.IsDir() {
				return nil
			}
			if item.Name() == "SKILL.md" {
				surfaces = append(surfaces, Surface{Path: relativeSlash(root, path), Kind: KindSkill})
			} else if strings.HasSuffix(item.Name(), ".md") && hasPathSegment(path, "references") {
				surfaces = append(surfaces, Surface{Path: relativeSlash(root, path), Kind: KindReference})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("discover %s: %w", displayPath(root, skillRoot), err)
		}
	}
	sort.Slice(surfaces, func(i, j int) bool { return surfaces[i].Path < surfaces[j].Path })
	return surfaces, nil
}

func validateCoverage(root string, entries []Entry, allowMissingGenerated bool) error {
	discovered, err := Discover(root)
	if err != nil {
		return err
	}
	actual := make(map[string]Kind, len(discovered))
	for _, surface := range discovered {
		actual[surface.Path] = surface.Kind
	}
	declared := map[string]struct {
		kind      Kind
		id        string
		generated bool
	}{}
	for _, entry := range entries {
		for _, output := range entry.Outputs {
			declared[output.Path] = struct {
				kind      Kind
				id        string
				generated bool
			}{entry.Kind, entry.ID, entry.Status == StatusPaired}
		}
	}

	var problems []string
	for path, kind := range actual {
		owner, ok := declared[path]
		if !ok {
			problems = append(problems, fmt.Sprintf("unclassified %s surface: %s", kind, path))
		} else if owner.kind != kind {
			problems = append(problems, fmt.Sprintf("%s: %s classifies %s as %s, discovered as %s", owner.id, path, path, owner.kind, kind))
		}
	}
	for path, owner := range declared {
		if _, ok := actual[path]; !ok && !(allowMissingGenerated && owner.generated) {
			problems = append(problems, fmt.Sprintf("%s: manifest output missing from discovery: %s", owner.id, path))
		}
	}
	return combineProblems(problems)
}

func relativeSlash(root, path string) string {
	rel, _ := filepath.Rel(root, path)
	return filepath.ToSlash(rel)
}

func hasPathSegment(path, wanted string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == wanted {
			return true
		}
	}
	return false
}

func classifySurfacePath(path string) (Kind, string, Format, bool) {
	if strings.HasPrefix(path, "plugins/agent-teams/roles/") {
		rel := strings.TrimPrefix(path, "plugins/agent-teams/roles/")
		if !strings.Contains(rel, "/") && strings.HasSuffix(rel, ".md") && rel != "README.md" {
			return KindRole, "claude", FormatMarkdown, true
		}
	}
	if strings.HasPrefix(path, "internal/verbs/codex_agents/") {
		rel := strings.TrimPrefix(path, "internal/verbs/codex_agents/")
		if !strings.Contains(rel, "/") && strings.HasSuffix(rel, ".toml") {
			return KindRole, "codex", FormatTOML, true
		}
	}
	for _, tree := range []struct {
		prefix  string
		runtime string
	}{
		{"plugins/agent-teams/skills/", "claude"},
		{"plugins/agent-teams-codex/skills/", "codex"},
	} {
		if !strings.HasPrefix(path, tree.prefix) {
			continue
		}
		rel := strings.TrimPrefix(path, tree.prefix)
		if strings.HasSuffix(rel, "/SKILL.md") && rel != "SKILL.md" {
			return KindSkill, tree.runtime, FormatMarkdown, true
		}
		if strings.HasSuffix(rel, ".md") && hasPathSegment(rel, "references") {
			return KindReference, tree.runtime, FormatMarkdown, true
		}
	}
	return "", "", "", false
}
