package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// HostFile is the file under the workspace home that persists this machine's
// host identity.
const HostFile = "host-id"

// hostname is the OS hostname source; tests replace it.
var hostname = os.Hostname

// Host returns this machine's host identity for the workspace at home:
// $AGENT_TEAMS_HOST if set and non-empty, else the id persisted in
// <home>/host-id. When that file is absent, Host seeds it with the current
// os.Hostname() (written atomically) and returns that value, so existing
// "where: host:" lines and to-host stamps keep matching. Later hostname
// changes, which macOS makes on a name conflict, no longer change the
// identity. If the file cannot be read or written, Host returns os.Hostname()
// for that call, or "" when the hostname cannot be read. Callers reach Host
// only on hosted workspaces, so a non-hosted home never gets the file
// (docs/hosted-beads.md, F5).
func Host(home string) string {
	if h := os.Getenv("AGENT_TEAMS_HOST"); h != "" {
		return h
	}
	path := filepath.Join(home, HostFile)
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id
		}
	}
	h, err := hostname()
	if err != nil || h == "" {
		return ""
	}
	writeHostFile(path, h)
	return h
}

// writeHostFile writes id to path through a temp file and rename. It ignores
// failures: the caller already holds the value to return.
func writeHostFile(path, id string) {
	tmp, err := os.CreateTemp(filepath.Dir(path), HostFile+".tmp-*")
	if err != nil {
		return
	}
	_, werr := tmp.WriteString(id + "\n")
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		os.Remove(tmp.Name())
	}
}
