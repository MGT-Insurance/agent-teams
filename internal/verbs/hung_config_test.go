package verbs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// restoreHungConfig snapshots the seven package-level tunables and restores
// them when the test ends. Every test here calls loadHungConfig, which
// assigns all seven; without this a configured value would leak into every
// subsequent test in the package.
func restoreHungConfig(t *testing.T) {
	t.Helper()
	tick, stuck, wake := hungTickInterval, hungStuckThreshold, hungWakeAttemptsBeforeDirectAlert
	flat, alert := hungWorkProductFlatThreshold, hungWorkProductAlertThreshold
	dead, window := hungDeadWorktreeThreshold, hungTranscriptCorroboratorWindow
	t.Cleanup(func() {
		hungTickInterval, hungStuckThreshold, hungWakeAttemptsBeforeDirectAlert = tick, stuck, wake
		hungWorkProductFlatThreshold, hungWorkProductAlertThreshold = flat, alert
		hungDeadWorktreeThreshold, hungTranscriptCorroboratorWindow = dead, window
	})
}

// writeHungConfig writes body as home's hung-config.json.
func writeHungConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, hungConfigFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// ── the compiled defaults ─────────────────────────────────────────────────────

// TestLoadHungConfig_NoEnvNoFile_AllDefaults pins the shipped defaults. A
// missing config file is the normal case and must be silent — a warning here
// would cry wolf on every relay start.
func TestLoadHungConfig_NoEnvNoFile_AllDefaults(t *testing.T) {
	restoreHungConfig(t)
	var out strings.Builder
	loadHungConfig(&out, t.TempDir())

	if hungTickInterval != 20*time.Minute {
		t.Errorf("tick_interval = %s, want 20m", hungTickInterval)
	}
	if hungStuckThreshold != 2*time.Hour {
		t.Errorf("stuck_threshold = %s, want 2h", hungStuckThreshold)
	}
	if hungWakeAttemptsBeforeDirectAlert != 2 {
		t.Errorf("wake_attempts_before_alert = %d, want 2", hungWakeAttemptsBeforeDirectAlert)
	}
	if hungWorkProductFlatThreshold != 2*time.Hour {
		t.Errorf("workproduct_flat_threshold = %s, want 2h", hungWorkProductFlatThreshold)
	}
	if hungWorkProductAlertThreshold != 4*time.Hour {
		t.Errorf("workproduct_alert_threshold = %s, want 4h", hungWorkProductAlertThreshold)
	}
	if hungDeadWorktreeThreshold != 2*time.Hour {
		t.Errorf("dead_worktree_threshold = %s, want 2h", hungDeadWorktreeThreshold)
	}
	if hungTranscriptCorroboratorWindow != 2*time.Hour {
		t.Errorf("transcript_corroborator_window = %s, want 2h", hungTranscriptCorroboratorWindow)
	}
	if out.String() != "" {
		t.Errorf("a missing config file must be silent, got %q", out.String())
	}
}

// TestHungDeadWorktreeThreshold_IsNotAnAliasOfStuck guards the trap that
// motivated giving it its own key: as a const it was `= hungStuckThreshold`,
// a live reference; as a var that alias would be a copy taken at
// package-init, before loadHungConfig runs. Configuring stuck alone must
// therefore leave dead-worktree at ITS default, not drag it along.
func TestHungDeadWorktreeThreshold_IsNotAnAliasOfStuck(t *testing.T) {
	restoreHungConfig(t)
	t.Setenv(envHungStuckThreshold, "37m")
	var out strings.Builder
	loadHungConfig(&out, t.TempDir())

	if hungStuckThreshold != 37*time.Minute {
		t.Errorf("stuck_threshold = %s, want 37m", hungStuckThreshold)
	}
	if hungDeadWorktreeThreshold != 2*time.Hour {
		t.Errorf("dead_worktree_threshold = %s, want its own 2h default — it must not track stuck_threshold", hungDeadWorktreeThreshold)
	}
}

// TestHungTranscriptCorroboratorWindow_IsNotAnAliasOfFlat is the same guard
// for the seventh value, which used to be a literal whose comment asserted
// it matched the flat threshold.
func TestHungTranscriptCorroboratorWindow_IsNotAnAliasOfFlat(t *testing.T) {
	restoreHungConfig(t)
	t.Setenv(envHungWorkProductFlatThreshold, "37m")
	var out strings.Builder
	loadHungConfig(&out, t.TempDir())

	if hungWorkProductFlatThreshold != 37*time.Minute {
		t.Errorf("workproduct_flat_threshold = %s, want 37m", hungWorkProductFlatThreshold)
	}
	if hungTranscriptCorroboratorWindow != 2*time.Hour {
		t.Errorf("transcript_corroborator_window = %s, want its own 2h default", hungTranscriptCorroboratorWindow)
	}
}

// ── resolution order, per value ───────────────────────────────────────────────

// TestLoadHungConfig_ResolutionOrder table-tests env > file > default for
// every one of the seven values, so no key can be wired to the wrong env var
// or json tag without a failure here.
func TestLoadHungConfig_ResolutionOrder(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envVal   string
		fileJSON string
		get      func() string
		want     string
	}{
		{"tick: default", "", "", "", func() string { return hungTickInterval.String() }, "20m0s"},
		{"tick: file beats default", "", "", `{"tick_interval":"45m"}`, func() string { return hungTickInterval.String() }, "45m0s"},
		{"tick: env beats file", envHungTickInterval, "90m", `{"tick_interval":"45m"}`, func() string { return hungTickInterval.String() }, "1h30m0s"},

		{"stuck: file beats default", "", "", `{"stuck_threshold":"45m"}`, func() string { return hungStuckThreshold.String() }, "45m0s"},
		{"stuck: env beats file", envHungStuckThreshold, "90m", `{"stuck_threshold":"45m"}`, func() string { return hungStuckThreshold.String() }, "1h30m0s"},

		{"wake attempts: file beats default", "", "", `{"wake_attempts_before_alert":5}`, func() string { return strconv.Itoa(hungWakeAttemptsBeforeDirectAlert) }, "5"},
		{"wake attempts: env beats file", envHungWakeAttemptsBeforeAlert, "7", `{"wake_attempts_before_alert":5}`, func() string { return strconv.Itoa(hungWakeAttemptsBeforeDirectAlert) }, "7"},

		{"flat: file beats default", "", "", `{"workproduct_flat_threshold":"45m"}`, func() string { return hungWorkProductFlatThreshold.String() }, "45m0s"},
		{"flat: env beats file", envHungWorkProductFlatThreshold, "90m", `{"workproduct_flat_threshold":"45m"}`, func() string { return hungWorkProductFlatThreshold.String() }, "1h30m0s"},

		{"alert: file beats default", "", "", `{"workproduct_alert_threshold":"45m"}`, func() string { return hungWorkProductAlertThreshold.String() }, "45m0s"},
		{"alert: env beats file", envHungWorkProductAlertThreshold, "90m", `{"workproduct_alert_threshold":"45m"}`, func() string { return hungWorkProductAlertThreshold.String() }, "1h30m0s"},

		{"dead worktree: file beats default", "", "", `{"dead_worktree_threshold":"45m"}`, func() string { return hungDeadWorktreeThreshold.String() }, "45m0s"},
		{"dead worktree: env beats file", envHungDeadWorktreeThreshold, "90m", `{"dead_worktree_threshold":"45m"}`, func() string { return hungDeadWorktreeThreshold.String() }, "1h30m0s"},

		{"corroborator: file beats default", "", "", `{"transcript_corroborator_window":"45m"}`, func() string { return hungTranscriptCorroboratorWindow.String() }, "45m0s"},
		{"corroborator: env beats file", envHungTranscriptCorroboratorWindow, "90m", `{"transcript_corroborator_window":"45m"}`, func() string { return hungTranscriptCorroboratorWindow.String() }, "1h30m0s"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restoreHungConfig(t)
			home := t.TempDir()
			if tc.fileJSON != "" {
				writeHungConfig(t, home, tc.fileJSON)
			}
			if tc.envKey != "" {
				t.Setenv(tc.envKey, tc.envVal)
			}
			var out strings.Builder
			loadHungConfig(&out, home)
			if got := tc.get(); got != tc.want {
				t.Errorf("resolved = %s, want %s", got, tc.want)
			}
			if out.String() != "" {
				t.Errorf("valid config must not warn, got %q", out.String())
			}
		})
	}
}

// ── degradation: bad config must never crash the relay ────────────────────────

// TestLoadHungConfig_MalformedJSON_AllDefaultsAndWarns covers the
// whole-file failure: the relay also routes every inbound human message, so
// a config typo must degrade to defaults rather than take the process down.
func TestLoadHungConfig_MalformedJSON_AllDefaultsAndWarns(t *testing.T) {
	restoreHungConfig(t)
	home := t.TempDir()
	writeHungConfig(t, home, "{banana")

	var out strings.Builder
	loadHungConfig(&out, home)

	if hungTickInterval != 20*time.Minute || hungStuckThreshold != 2*time.Hour {
		t.Errorf("malformed JSON must yield all defaults, got tick=%s stuck=%s", hungTickInterval, hungStuckThreshold)
	}
	if !strings.Contains(out.String(), hungConfigFileName) {
		t.Errorf("warning must name the config file, got %q", out.String())
	}
}

// TestLoadHungConfig_OneBadValue_DegradesAlone is the per-field fallback the
// contract requires: a single unparseable value falls back to ITS default
// while the rest of a valid file still applies. Whole-file rejection here
// would be a bug — it would silently discard good config.
func TestLoadHungConfig_OneBadValue_DegradesAlone(t *testing.T) {
	restoreHungConfig(t)
	home := t.TempDir()
	writeHungConfig(t, home, `{"stuck_threshold":"banana","tick_interval":"45m"}`)

	var out strings.Builder
	loadHungConfig(&out, home)

	if hungStuckThreshold != 2*time.Hour {
		t.Errorf("stuck_threshold = %s, want the 2h default (its value was unparseable)", hungStuckThreshold)
	}
	if hungTickInterval != 45*time.Minute {
		t.Errorf("tick_interval = %s, want 45m — a sibling's bad value must not discard it", hungTickInterval)
	}
	warning := out.String()
	if !strings.Contains(warning, "stuck_threshold") || !strings.Contains(warning, "banana") {
		t.Errorf("warning must name the key and the offending text, got %q", warning)
	}
	if strings.Contains(warning, "tick_interval") {
		t.Errorf("the good value must not be warned about, got %q", warning)
	}
}

// TestLoadHungConfig_TopLevelJSONWrongShape covers a config file that is
// syntactically valid JSON but not a JSON object — an array or a bare
// string. json.Unmarshal rejects both into hungConfigFile with a type error,
// which readHungConfigFile must treat the same as malformed JSON: warn,
// name the file, and fall through to every default (not partially decoded
// garbage).
func TestLoadHungConfig_TopLevelJSONWrongShape(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"JSON array", `["tick_interval","45m"]`},
		{"bare JSON string", `"45m"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restoreHungConfig(t)
			home := t.TempDir()
			writeHungConfig(t, home, tc.body)

			var out strings.Builder
			loadHungConfig(&out, home)

			if hungTickInterval != 20*time.Minute || hungStuckThreshold != 2*time.Hour {
				t.Errorf("wrong-shape JSON must yield all defaults, got tick=%s stuck=%s", hungTickInterval, hungStuckThreshold)
			}
			if !strings.Contains(out.String(), hungConfigFileName) {
				t.Errorf("warning must name the config file, got %q", out.String())
			}
		})
	}
}

// TestLoadHungConfig_NoUnitDuration_WarnsAndDefaults covers the bare-number
// case an operator might reasonably try ("20" meaning "20 minutes"):
// time.ParseDuration requires a unit, so this must warn and default rather
// than silently guessing a unit that's wrong at one end of the 20m..4h
// range or the other.
func TestLoadHungConfig_NoUnitDuration_WarnsAndDefaults(t *testing.T) {
	restoreHungConfig(t)
	home := t.TempDir()
	writeHungConfig(t, home, `{"tick_interval":"20"}`)

	var out strings.Builder
	loadHungConfig(&out, home)

	if hungTickInterval != 20*time.Minute {
		t.Errorf("tick_interval = %s, want the 20m default (a bare number has no unit)", hungTickInterval)
	}
	warning := out.String()
	if !strings.Contains(warning, "tick_interval") || !strings.Contains(warning, `"20"`) {
		t.Errorf("warning must name the key and quote the offending value, got %q", warning)
	}
}

// TestLoadHungConfig_EmptyStringValue_SilentlyDefaults covers an explicit
// "" value, distinct from an unparseable one: hungConfigSource treats an
// empty (post-trim) string as "no value supplied" at that tier, same as the
// key being absent — so this must default SILENTLY, not warn. Getting this
// wrong (warning here) would cry wolf on a template config file that lists
// every key with blank placeholder values.
func TestLoadHungConfig_EmptyStringValue_SilentlyDefaults(t *testing.T) {
	restoreHungConfig(t)
	home := t.TempDir()
	writeHungConfig(t, home, `{"stuck_threshold":""}`)

	var out strings.Builder
	loadHungConfig(&out, home)

	if hungStuckThreshold != 2*time.Hour {
		t.Errorf("stuck_threshold = %s, want the 2h default", hungStuckThreshold)
	}
	if out.String() != "" {
		t.Errorf("an empty string value must default silently, got warning %q", out.String())
	}
}

// TestLoadHungConfig_WhitespaceIsTrimmed covers surrounding whitespace on
// both tiers: hungConfigSource trims the file value and resolveHungDuration
// receives an already-trimmed env value via os.Getenv+TrimSpace. Padding a
// value (a stray newline or trailing space from a hand-edited file) must
// resolve like the clean value, not warn or fall back to default.
func TestLoadHungConfig_WhitespaceIsTrimmed(t *testing.T) {
	t.Run("file value", func(t *testing.T) {
		restoreHungConfig(t)
		home := t.TempDir()
		writeHungConfig(t, home, `{"tick_interval":"  45m  "}`)

		var out strings.Builder
		loadHungConfig(&out, home)

		if hungTickInterval != 45*time.Minute {
			t.Errorf("tick_interval = %s, want 45m (padding must be trimmed)", hungTickInterval)
		}
		if out.String() != "" {
			t.Errorf("a padded-but-valid value must not warn, got %q", out.String())
		}
	})

	t.Run("env value", func(t *testing.T) {
		restoreHungConfig(t)
		t.Setenv(envHungTickInterval, "  45m  ")

		var out strings.Builder
		loadHungConfig(&out, t.TempDir())

		if hungTickInterval != 45*time.Minute {
			t.Errorf("tick_interval = %s, want 45m (padding must be trimmed)", hungTickInterval)
		}
		if out.String() != "" {
			t.Errorf("a padded-but-valid value must not warn, got %q", out.String())
		}
	})
}

// TestLoadHungConfig_UnreadableConfigFile_DegradesAndWarns covers a config
// file that exists but can't be opened (e.g. AGENT_TEAMS_HOME landed on a
// permissions-restricted mount) — a real os.ReadFile error distinct from
// os.ErrNotExist, which readHungConfigFile must warn on and degrade from,
// same as malformed JSON, rather than propagating the error and taking the
// relay down.
func TestLoadHungConfig_UnreadableConfigFile_DegradesAndWarns(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root ignores file permissions")
	}
	restoreHungConfig(t)
	home := t.TempDir()
	writeHungConfig(t, home, `{"tick_interval":"45m"}`)
	path := filepath.Join(home, hungConfigFileName)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })

	var out strings.Builder
	loadHungConfig(&out, home)

	if hungTickInterval != 20*time.Minute {
		t.Errorf("tick_interval = %s, want the 20m default (file was unreadable)", hungTickInterval)
	}
	if !strings.Contains(out.String(), hungConfigFileName) {
		t.Errorf("warning must name the config file, got %q", out.String())
	}
}

// TestLoadHungConfig_RejectsNonPositive covers the values that parse but are
// nonsense. A zero tick interval would panic time.NewTicker; a negative
// threshold would make everything instantly hung.
func TestLoadHungConfig_RejectsNonPositive(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		envVal string
		check  func(*testing.T)
	}{
		{"zero duration", envHungTickInterval, "0s", func(t *testing.T) {
			if hungTickInterval != 20*time.Minute {
				t.Errorf("tick_interval = %s, want the 20m default", hungTickInterval)
			}
		}},
		{"negative duration", envHungStuckThreshold, "-5m", func(t *testing.T) {
			if hungStuckThreshold != 2*time.Hour {
				t.Errorf("stuck_threshold = %s, want the 2h default", hungStuckThreshold)
			}
		}},
		{"zero count", envHungWakeAttemptsBeforeAlert, "0", func(t *testing.T) {
			if hungWakeAttemptsBeforeDirectAlert != 2 {
				t.Errorf("wake_attempts_before_alert = %d, want the default 2", hungWakeAttemptsBeforeDirectAlert)
			}
		}},
		{"non-numeric count", envHungWakeAttemptsBeforeAlert, "banana", func(t *testing.T) {
			if hungWakeAttemptsBeforeDirectAlert != 2 {
				t.Errorf("wake_attempts_before_alert = %d, want the default 2", hungWakeAttemptsBeforeDirectAlert)
			}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restoreHungConfig(t)
			t.Setenv(tc.envKey, tc.envVal)
			var out strings.Builder
			loadHungConfig(&out, t.TempDir())
			tc.check(t)
			if !strings.Contains(out.String(), tc.envKey) {
				t.Errorf("warning must name the source, got %q", out.String())
			}
		})
	}
}

// TestLoadHungConfig_NonPositiveCountInFile is the file-tier half of the
// above: json.Unmarshal accepts 0 into an *int, so the positivity check has
// to happen after decoding, not only on the env path.
func TestLoadHungConfig_NonPositiveCountInFile(t *testing.T) {
	restoreHungConfig(t)
	home := t.TempDir()
	writeHungConfig(t, home, `{"wake_attempts_before_alert":0}`)

	var out strings.Builder
	loadHungConfig(&out, home)

	if hungWakeAttemptsBeforeDirectAlert != 2 {
		t.Errorf("wake_attempts_before_alert = %d, want the default 2", hungWakeAttemptsBeforeDirectAlert)
	}
	if !strings.Contains(out.String(), "wake_attempts_before_alert") {
		t.Errorf("warning must name the key, got %q", out.String())
	}
}

// TestHungConfigSummary_NamesEverySettableKey guards the startup log line the
// shell test (agent-teams-rhnc.3) and any operator reads: every key must
// appear with its RESOLVED value, keyed by the name you would actually put
// in hung-config.json.
func TestHungConfigSummary_NamesEverySettableKey(t *testing.T) {
	restoreHungConfig(t)
	home := t.TempDir()
	writeHungConfig(t, home, `{"tick_interval":"45m"}`)
	loadHungConfig(&strings.Builder{}, home)

	summary := hungConfigSummary()
	want := []string{
		"tick_interval=45m0s",
		"stuck_threshold=2h0m0s",
		"wake_attempts_before_alert=2",
		"workproduct_flat_threshold=2h0m0s",
		"workproduct_alert_threshold=4h0m0s",
		"dead_worktree_threshold=2h0m0s",
		"transcript_corroborator_window=2h0m0s",
	}
	for _, w := range want {
		if !strings.Contains(summary, w) {
			t.Errorf("summary missing %q; got %q", w, summary)
		}
	}
}

// ── the verification bar: a configured value reaches the RUNNING ticker ───────

// TestRunHungTick_ConfiguredIntervalReachesTheTicker is the proof this whole
// initiative rests on.
//
// Asserting hungTickInterval == 20*time.Minute would show only that a
// variable holds a value; time.NewTicker's argument is not readable back, so
// nothing in an arithmetic test says the ticker was handed it. Here the
// interval is set THROUGH THE REAL RESOLUTION PATH (env var + loadHungConfig,
// not a direct poke at the var) and the ticks that actually arrive are
// counted.
//
// The negative half is what makes it a proof rather than a smoke test: with
// the env unset, the same code in the same window must produce ZERO ticks,
// because the 20m default cannot fire that fast. Without it, a ticker wired
// to any small hardcoded interval would pass.
func TestRunHungTick_ConfiguredIntervalReachesTheTicker(t *testing.T) {
	const (
		configured = 20 * time.Millisecond
		window     = 2 * time.Second
		wantTicks  = 3
	)

	run := func(t *testing.T, setEnv bool) int {
		t.Helper()
		restoreHungConfig(t)

		if setEnv {
			t.Setenv(envHungTickInterval, configured.String())
		} else {
			t.Setenv(envHungTickInterval, "")
		}
		loadHungConfig(&strings.Builder{}, t.TempDir())

		origTick := hungTickFunc
		defer func() { hungTickFunc = origTick }()

		var count atomic.Int64
		reached := make(chan struct{})
		var once sync.Once
		hungTickFunc = func(*cli.Context, hungTickDeps) error {
			if count.Add(1) >= wantTicks {
				once.Do(func() { close(reached) })
			}
			return nil
		}

		stop := make(chan struct{})
		done := make(chan struct{})
		ctx := &cli.Context{Home: t.TempDir(), Stdout: &strings.Builder{}, Stderr: &strings.Builder{}}
		go func() {
			defer close(done)
			runHungTickUntil(ctx, nil, stop)
		}()

		select {
		case <-reached:
		case <-time.After(window):
		}
		// Join before the deferred restore: a leaked goroutine would resume
		// calling the REAL doHungTick every 20ms once hungTickFunc is put back.
		close(stop)
		<-done

		return int(count.Load())
	}

	t.Run("configured 20ms interval drives the ticker", func(t *testing.T) {
		if got := run(t, true); got < wantTicks {
			t.Fatalf("got %d ticks in %s at a configured %s interval, want >= %d — the configured value did not reach time.NewTicker", got, window, configured, wantTicks)
		}
	})

	t.Run("env unset: the 20m default cannot fire in the same window", func(t *testing.T) {
		if got := run(t, false); got != 0 {
			t.Fatalf("got %d ticks in %s at the 20m default, want 0 — the ticker is not honouring the resolved interval", got, window)
		}
	})
}
