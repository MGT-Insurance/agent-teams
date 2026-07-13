//go:build e2e

package main

import (
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/transport/stub"
)

// registerE2ETransports registers the stub transport under the name "stub".
// This file is gated by the e2e build tag and is not included in production
// binaries.
func registerE2ETransports() {
	transport.RegisterTransport("stub", stub.Factory)
}
