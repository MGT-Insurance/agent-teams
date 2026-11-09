package main

import (
	"os"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/testenv"
)

// TestMain strips the inherited DSP_* variables so verbs run through run()
// never write to a cloud Dispatcher run's real outbox.
func TestMain(m *testing.M) {
	testenv.UnsetDispatcherEnv()
	os.Exit(m.Run())
}
