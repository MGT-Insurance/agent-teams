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
	WorkRuntime       *string `toml:"work_runtime"`
	ReviewRuntime     *string `toml:"review_runtime"`
	AutoCompactWindow *int64  `toml:"auto_compact_window"`
}

// RuntimeDefault reads config.toml beneath home and returns the selected
// dispatch-class default. Missing files and missing selected keys have no
// default. Every present key is validated so an invalid strict document never
// becomes usable merely because the invalid key was not selected.
func RuntimeDefault(home string, class RuntimeClass) (string, bool, error) {
	cfg, path, err := readConfig(home)
	if err != nil {
		return "", false, err
	}

	switch class {
	case WorkRuntime:
		return validateRuntime(path, WorkRuntime, cfg.WorkRuntime)
	case ReviewRuntime:
		return validateRuntime(path, ReviewRuntime, cfg.ReviewRuntime)
	default:
		return "", false, fmt.Errorf("runtime config %s: unknown dispatch class %q", path, class)
	}
}

// AutoCompactWindow reads config.toml beneath home and returns the optional
// token limit for managed Codex threads. The value must be a positive signed
// 64-bit TOML integer. As with RuntimeDefault, every present key is validated.
func AutoCompactWindow(home string) (int64, bool, error) {
	cfg, _, err := readConfig(home)
	if err != nil {
		return 0, false, err
	}
	if cfg.AutoCompactWindow == nil {
		return 0, false, nil
	}
	return *cfg.AutoCompactWindow, true, nil
}

func readConfig(home string) (config, string, error) {
	path := filepath.Join(home, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, lstatErr := os.Lstat(path)
			switch {
			case lstatErr == nil:
				return config{}, path, fmt.Errorf("read runtime config %s: %w", path, err)
			case errors.Is(lstatErr, os.ErrNotExist):
				return config{}, path, nil
			default:
				return config{}, path, fmt.Errorf("inspect runtime config %s after read failure: %w", path, lstatErr)
			}
		}
		return config{}, path, fmt.Errorf("read runtime config %s: %w", path, err)
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
			return config{}, path, fmt.Errorf("parse runtime config %s: unknown key or table %q", path, strings.Join(keys, ", "))
		}
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			line, column := decodeErr.Position()
			if keys := knownKeyContext(data); keys != "" {
				return config{}, path, fmt.Errorf("parse runtime config %s: invalid strict TOML at line %d, column %d near known key %q", path, line, column, keys)
			}
			return config{}, path, fmt.Errorf("parse runtime config %s: invalid strict TOML at line %d, column %d", path, line, column)
		}
		if keys := knownKeyContext(data); keys != "" {
			return config{}, path, fmt.Errorf("parse runtime config %s: invalid strict TOML near key %q", path, keys)
		}
		return config{}, path, fmt.Errorf("parse runtime config %s: invalid strict TOML", path)
	}

	if _, _, err := validateRuntime(path, WorkRuntime, cfg.WorkRuntime); err != nil {
		return config{}, path, err
	}
	if _, _, err := validateRuntime(path, ReviewRuntime, cfg.ReviewRuntime); err != nil {
		return config{}, path, err
	}
	if cfg.AutoCompactWindow != nil && *cfg.AutoCompactWindow <= 0 {
		return config{}, path, fmt.Errorf("runtime config %s: auto_compact_window must be a positive signed 64-bit integer", path)
	}
	return cfg, path, nil
}

func knownKeyContext(data []byte) string {
	var keys []string
	for _, key := range []string{string(WorkRuntime), string(ReviewRuntime), "auto_compact_window"} {
		if bytes.Contains(data, []byte(key)) {
			keys = append(keys, key)
		}
	}
	return strings.Join(keys, ", ")
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
