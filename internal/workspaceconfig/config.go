// Package workspaceconfig reads the agent-teams machine-local configuration.
package workspaceconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
	"github.com/pelletier/go-toml/v2"
)

const FileName = "config.toml"

// RuntimeClass selects the runtime default used by one dispatch class. Its
// values are also the only runtime keys accepted in config.toml.
type RuntimeClass string

const (
	WorkRuntime   RuntimeClass = "work_runtime"
	ReviewRuntime RuntimeClass = "review_runtime"
)

type config struct {
	WorkRuntime   *string `toml:"work_runtime"`
	ReviewRuntime *string `toml:"review_runtime"`
}

// RuntimeDefault reads config.toml beneath home and returns the selected
// dispatch-class default. Missing files and missing selected keys have no
// default. Every present key is validated so an invalid strict document never
// becomes usable merely because the invalid key was not selected.
func RuntimeDefault(home string, class RuntimeClass) (string, bool, error) {
	path := filepath.Join(home, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read runtime config %s: %w", path, err)
	}

	var cfg config
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		var unknown *toml.StrictMissingError
		if errors.As(err, &unknown) {
			keys := make([]string, 0, len(unknown.Errors))
			for _, field := range unknown.Errors {
				keys = append(keys, strings.Join(field.Key(), "."))
			}
			return "", false, fmt.Errorf("parse runtime config %s: unknown key or table %q", path, strings.Join(keys, ", "))
		}
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			line, column := decodeErr.Position()
			return "", false, fmt.Errorf("parse runtime config %s: invalid strict TOML at line %d, column %d", path, line, column)
		}
		return "", false, fmt.Errorf("parse runtime config %s: invalid strict TOML", path)
	}

	work, workSet, err := validateRuntime(path, WorkRuntime, cfg.WorkRuntime)
	if err != nil {
		return "", false, err
	}
	review, reviewSet, err := validateRuntime(path, ReviewRuntime, cfg.ReviewRuntime)
	if err != nil {
		return "", false, err
	}

	switch class {
	case WorkRuntime:
		return work, workSet, nil
	case ReviewRuntime:
		return review, reviewSet, nil
	default:
		return "", false, fmt.Errorf("runtime config %s: unknown dispatch class %q", path, class)
	}
}

func validateRuntime(path string, key RuntimeClass, value *string) (string, bool, error) {
	if value == nil {
		return "", false, nil
	}
	kind, err := sessionruntime.ParseKind(*value)
	if err != nil || string(kind) != *value {
		return "", false, fmt.Errorf("runtime config %s: %s must be exactly %q or %q", path, key, sessionruntime.Claude, sessionruntime.Codex)
	}
	return string(kind), true, nil
}
