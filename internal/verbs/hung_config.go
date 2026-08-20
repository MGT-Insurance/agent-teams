// Package verbs — hung_config.go is the ONLY seam that reads operator
// configuration for stall detection (agent-teams-rhnc.1).
//
// The eight tunables in hung_scan.go / hung_tick.go / hung_workproduct.go
// were compiled-in constants, so retuning them meant a source edit, a
// rebuild of the four committed platform binaries, and a plugin version
// bump. This file makes them settable at process start.
//
// Deliberately NOT the plugin userConfig chain (plugin.json userConfig ->
// export-plugin-options.sh -> CLAUDE_PLUGIN_OPTION_*). That chain only
// populates the environment of Bash tool calls made inside a Claude Code
// session, and the relay is routinely hand-started from a terminal with no
// CLAUDE_* variables at all — so a userConfig knob would work or not work
// depending on how the relay happened to be launched that day, with no
// signal either way. A knob that silently is not connected is worse than no
// knob.
//
// The FILE tier is the primary mechanism, and it works on every launch path
// unconditionally: AGENT_TEAMS_HOME is pinned explicitly by the supervised
// spawner (relay_supervise.go's defaultRelaySpawn) and defaults correctly
// for a hand-started relay, so workspace.Home() resolves to the same
// workspace either way. The env tier is the convenience override for a
// one-off run, not the mechanism operators are expected to reach for.
package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// hungConfigFileName is the single JSON object, under the workspace home,
// holding every stall-detection tunable. One file rather than one file per
// value so an operator can `cat` the whole tuning state at once.
const hungConfigFileName = "hung-config.json"

// Compiled defaults for the eight tunables — the last tier of the
// env -> file -> default resolution, and the value each var is initialized
// to so that code paths which never call loadHungConfig (every existing
// unit test) behave deterministically.
//
// defaultHungDeadWorktreeThreshold deliberately states its value literally
// rather than aliasing defaultHungStuckThreshold, even though the two are
// equal today: the point of giving it its own key is that an operator can
// move them apart, and an alias would quietly re-couple them.
//
// defaultHungSuspendGapMultiplier (agent-teams-ndr4.2) is the tick loop's
// own suspend detector: the real ticker (runHungTickUntil, hung_tick.go)
// fires every exactly hungTickInterval, so a gap between two consecutive
// doHungTick invocations far exceeding that interval can only mean the OS
// process itself was suspended (laptop sleep) for the excess, not that
// anything took long inside one tick. 3x leaves headroom for an ordinary
// slow tick (a busy machine, a slow `bd list`) without mistaking it for a
// suspend.
const (
	defaultHungTickInterval                 = 20 * time.Minute
	defaultHungStuckThreshold               = 2 * time.Hour
	defaultHungWakeAttemptsBeforeAlert      = 2
	defaultHungWorkProductFlatThreshold     = 2 * time.Hour
	defaultHungWorkProductAlertThreshold    = 4 * time.Hour
	defaultHungDeadWorktreeThreshold        = 2 * time.Hour
	defaultHungTranscriptCorroboratorWindow = 2 * time.Hour
	defaultHungSuspendGapMultiplier         = 3
)

// Environment variables overriding each value, checked before the file.
const (
	envHungTickInterval                 = "AGENT_TEAMS_HUNG_TICK_INTERVAL"
	envHungStuckThreshold               = "AGENT_TEAMS_HUNG_STUCK_THRESHOLD"
	envHungWakeAttemptsBeforeAlert      = "AGENT_TEAMS_HUNG_WAKE_ATTEMPTS_BEFORE_ALERT"
	envHungWorkProductFlatThreshold     = "AGENT_TEAMS_HUNG_WORKPRODUCT_FLAT_THRESHOLD"
	envHungWorkProductAlertThreshold    = "AGENT_TEAMS_HUNG_WORKPRODUCT_ALERT_THRESHOLD"
	envHungDeadWorktreeThreshold        = "AGENT_TEAMS_HUNG_DEAD_WORKTREE_THRESHOLD"
	envHungTranscriptCorroboratorWindow = "AGENT_TEAMS_HUNG_TRANSCRIPT_CORROBORATOR_WINDOW"
	envHungSuspendGapMultiplier         = "AGENT_TEAMS_HUNG_SUSPEND_GAP_MULTIPLIER"
)

// hungConfigFile is the on-disk shape of <home>/hung-config.json.
//
// Every field is a POINTER so an absent key is distinguishable from a zero
// value — a missing "tick_interval" must fall through to the default, not
// resolve to 0 — and so one unparseable value can degrade on its own while
// the other six still apply.
//
// Durations are Go duration strings ("20m", "2h") parsed with
// time.ParseDuration: stdlib, and unambiguous where a bare number would
// force the reader to guess a unit that is wrong at one end or the other of
// a 20m..4h range.
type hungConfigFile struct {
	TickInterval                 *string `json:"tick_interval"`
	StuckThreshold               *string `json:"stuck_threshold"`
	WakeAttemptsBeforeAlert      *int    `json:"wake_attempts_before_alert"`
	WorkProductFlatThreshold     *string `json:"workproduct_flat_threshold"`
	WorkProductAlertThreshold    *string `json:"workproduct_alert_threshold"`
	DeadWorktreeThreshold        *string `json:"dead_worktree_threshold"`
	TranscriptCorroboratorWindow *string `json:"transcript_corroborator_window"`
	SuspendGapMultiplier         *int    `json:"suspend_gap_multiplier"`
}

// loadHungConfig resolves all eight tunables and assigns them to their
// package-level vars. Call it ONCE per process, before anything reads them:
// relayKong.Run (before starting the tick goroutine) and hungScanKong.Run
// (before scanHung), so `ateam hung-scan` reports against exactly the
// thresholds the relay acts on.
//
// It NEVER fails and never panics. The relay is an unsupervised singleton
// that also routes every inbound human message, so a typo in a config file
// must degrade to defaults with a warning, never take down message routing.
// Warnings go to w; a missing config file is normal and silent.
func loadHungConfig(w io.Writer, home string) {
	file := readHungConfigFile(w, home)

	hungTickInterval = resolveHungDuration(w, envHungTickInterval, "tick_interval", file.TickInterval, defaultHungTickInterval)
	hungStuckThreshold = resolveHungDuration(w, envHungStuckThreshold, "stuck_threshold", file.StuckThreshold, defaultHungStuckThreshold)
	hungWakeAttemptsBeforeDirectAlert = resolveHungInt(w, envHungWakeAttemptsBeforeAlert, "wake_attempts_before_alert", file.WakeAttemptsBeforeAlert, defaultHungWakeAttemptsBeforeAlert)
	hungWorkProductFlatThreshold = resolveHungDuration(w, envHungWorkProductFlatThreshold, "workproduct_flat_threshold", file.WorkProductFlatThreshold, defaultHungWorkProductFlatThreshold)
	hungWorkProductAlertThreshold = resolveHungDuration(w, envHungWorkProductAlertThreshold, "workproduct_alert_threshold", file.WorkProductAlertThreshold, defaultHungWorkProductAlertThreshold)
	hungDeadWorktreeThreshold = resolveHungDuration(w, envHungDeadWorktreeThreshold, "dead_worktree_threshold", file.DeadWorktreeThreshold, defaultHungDeadWorktreeThreshold)
	hungTranscriptCorroboratorWindow = resolveHungDuration(w, envHungTranscriptCorroboratorWindow, "transcript_corroborator_window", file.TranscriptCorroboratorWindow, defaultHungTranscriptCorroboratorWindow)
	hungSuspendGapMultiplier = resolveHungInt(w, envHungSuspendGapMultiplier, "suspend_gap_multiplier", file.SuspendGapMultiplier, defaultHungSuspendGapMultiplier)
}

// readHungConfigFile decodes <home>/hung-config.json. An absent file is the
// normal case and yields an empty (all-nil) struct silently. Anything else
// that goes wrong — unreadable file, malformed JSON, a value of the wrong
// JSON type — warns and yields the same empty struct, so every value falls
// through to its env override or compiled default.
func readHungConfigFile(w io.Writer, home string) hungConfigFile {
	var cfg hungConfigFile
	if home == "" {
		return cfg
	}
	path := filepath.Join(home, hungConfigFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg
	}
	if err != nil {
		transport.Logf(w, 0, "hung config: %s: %v; using defaults for all values", path, err)
		return hungConfigFile{}
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		transport.Logf(w, 0, "hung config: %s: %v; using defaults for all values", path, err)
		return hungConfigFile{}
	}
	return cfg
}

// resolveHungDuration applies env -> file -> def for one duration value.
// An unparseable or non-positive value warns — naming the key, the source
// and the offending text, so the warning alone is enough to fix the config —
// and falls back to def without disturbing the other six values.
func resolveHungDuration(w io.Writer, envKey, jsonKey string, fileVal *string, def time.Duration) time.Duration {
	raw, source := hungConfigSource(envKey, fileVal)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		transport.Logf(w, 0, "hung config: %s %s=%q is not a duration (%v); using default %s", source, jsonKey, raw, err, def)
		return def
	}
	if d <= 0 {
		transport.Logf(w, 0, "hung config: %s %s=%q must be positive; using default %s", source, jsonKey, raw, def)
		return def
	}
	return d
}

// resolveHungInt is resolveHungDuration's sibling for the one count-valued
// tunable. The file tier is already an int (json.Unmarshal rejected anything
// else at the file level); only the env tier needs parsing.
func resolveHungInt(w io.Writer, envKey, jsonKey string, fileVal *int, def int) int {
	if raw := strings.TrimSpace(os.Getenv(envKey)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			transport.Logf(w, 0, "hung config: env %s=%q is not an integer (%v); using default %d", envKey, raw, err, def)
			return def
		}
		if n <= 0 {
			transport.Logf(w, 0, "hung config: env %s=%q must be positive; using default %d", envKey, raw, def)
			return def
		}
		return n
	}
	if fileVal != nil {
		if *fileVal <= 0 {
			transport.Logf(w, 0, "hung config: file %s=%d must be positive; using default %d", jsonKey, *fileVal, def)
			return def
		}
		return *fileVal
	}
	return def
}

// hungConfigSource returns the highest-priority raw text for one value and a
// label naming where it came from, for use in warnings. An empty string means
// neither tier supplied a value.
func hungConfigSource(envKey string, fileVal *string) (raw, source string) {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v, "env " + envKey + ":"
	}
	if fileVal != nil {
		return strings.TrimSpace(*fileVal), "file " + hungConfigFileName + ":"
	}
	return "", ""
}

// hungConfigSummary renders the RESOLVED value of all eight tunables, keyed
// by the json key an operator would set to change each one. The relay logs
// it at startup: because config is read once at process start, this line is
// the only way to confirm a hand-started relay actually picked up an edit.
func hungConfigSummary() string {
	return fmt.Sprintf(
		"tick_interval=%s stuck_threshold=%s wake_attempts_before_alert=%d workproduct_flat_threshold=%s workproduct_alert_threshold=%s dead_worktree_threshold=%s transcript_corroborator_window=%s suspend_gap_multiplier=%d",
		hungTickInterval,
		hungStuckThreshold,
		hungWakeAttemptsBeforeDirectAlert,
		hungWorkProductFlatThreshold,
		hungWorkProductAlertThreshold,
		hungDeadWorktreeThreshold,
		hungTranscriptCorroboratorWindow,
		hungSuspendGapMultiplier,
	)
}
