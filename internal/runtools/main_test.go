package runtools

import (
	"os"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/testenv"
)

// TestMain strips the inherited DSP_* variables so a test run inside a cloud
// Dispatcher run never posts to that run's real /api/run-tools endpoint.
func TestMain(m *testing.M) {
	testenv.UnsetDispatcherEnv()
	os.Exit(m.Run())
}
