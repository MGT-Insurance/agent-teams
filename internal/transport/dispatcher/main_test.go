package dispatcher_test

import (
	"os"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/testenv"
)

// TestMain strips the inherited DSP_* variables so a test run inside a cloud
// Dispatcher run never talks to that run's real Dispatcher.
func TestMain(m *testing.M) {
	testenv.UnsetDispatcherEnv()
	os.Exit(m.Run())
}
