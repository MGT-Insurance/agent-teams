//go:build e2e

package main

// Import the stub transport so its init() registers it under the name "stub".
// This file is gated by the e2e build tag and is not included in production
// binaries.
import _ "github.com/mgt-insurance/agent-teams/internal/transport/stub"
