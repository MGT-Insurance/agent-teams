package promptsync

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

const SkillUTF16Limit = 20002

type SkillMeasurement struct {
	LogicalID       string
	Path            string
	RenderedUTF16   int
	Headroom        int
	MinimumHeadroom int
}

func measureSkill(logicalID, path string, rendered []byte, budget SkillBudget) (SkillMeasurement, error) {
	body, err := stripFrontmatter(rendered)
	if err != nil {
		return SkillMeasurement{}, fmt.Errorf("%s: generated SKILL.md %s: %w", logicalID, path, err)
	}
	baseLine := "Base directory for this skill: " + filepath.ToSlash(budget.BaseDirectory) + "\n\n"
	units := len(utf16.Encode([]rune(baseLine + string(body))))
	measurement := SkillMeasurement{
		LogicalID:       logicalID,
		Path:            path,
		RenderedUTF16:   units,
		Headroom:        SkillUTF16Limit - units,
		MinimumHeadroom: budget.MinHeadroom,
	}
	if measurement.Headroom < budget.MinHeadroom {
		return measurement, fmt.Errorf("%s: generated SKILL.md %s has %d UTF-16 units and %d headroom to %d; requires at least %d headroom", logicalID, path, units, measurement.Headroom, SkillUTF16Limit, budget.MinHeadroom)
	}
	return measurement, nil
}

func stripFrontmatter(content []byte) ([]byte, error) {
	text := string(content)
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("missing opening YAML frontmatter delimiter")
	}
	closing := strings.Index(text[4:], "\n---\n")
	if closing < 0 {
		return nil, fmt.Errorf("missing closing YAML frontmatter delimiter")
	}
	return []byte(text[4+closing+5:]), nil
}
