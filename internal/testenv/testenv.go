// Package testenv keeps tests from inheriting a Dispatcher run's environment.
package testenv

import (
	"os"
	"strings"
)

// UnsetDispatcherEnv removes every DSP_* variable from the process
// environment. Call it from TestMain before m.Run: inside a cloud Dispatcher
// run the DSP_* variables are set, and production readers (the outbox writer,
// the run-tools client, the dispatcher transport) would otherwise send test
// fixtures to the real run. Tests that need a DSP_* value set it with
// t.Setenv.
func UnsetDispatcherEnv() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "DSP_") {
			os.Unsetenv(name)
		}
	}
}
