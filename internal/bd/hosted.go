package bd

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// HostedServer reports whether the workspace at home is bound to a hosted
// (remote) Dolt server, and returns that server's host. The selector lives in
// <home>/.beads/metadata.json, never in process env (docs/hosted-beads.md, F1,
// F3): ok is true iff dolt_mode is "server" and dolt_server_host is non-empty
// and not a loopback address. A missing or unparseable file returns ("",
// false), which keeps the embedded behavior.
func HostedServer(home string) (host string, ok bool) {
	data, err := os.ReadFile(filepath.Join(home, ".beads", "metadata.json"))
	if err != nil {
		return "", false
	}
	var meta struct {
		DoltMode       string `json:"dolt_mode"`
		DoltServerHost string `json:"dolt_server_host"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", false
	}
	if meta.DoltMode != "server" {
		return "", false
	}
	switch meta.DoltServerHost {
	case "", "127.0.0.1", "localhost", "::1":
		return "", false
	}
	return meta.DoltServerHost, true
}
