//go:build !e2e

package main

// registerE2ETransports is a no-op in production builds. The e2e-tagged
// build (cmd/ateam/e2e_transports.go) overrides this to register the stub
// transport.
func registerE2ETransports() {}
