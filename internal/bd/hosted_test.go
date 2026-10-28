package bd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostedServer(t *testing.T) {
	const hosted = "mgt-spike.dbs.hosted.doltdb.com"
	cases := []struct {
		name     string
		metadata *string // nil: no file
		wantHost string
		wantOK   bool
	}{
		{"hosted server", ptr(`{"dolt_mode":"server","dolt_server_host":"` + hosted + `"}`), hosted, true},
		{"embedded mode", ptr(`{"dolt_mode":"embedded","dolt_server_host":"` + hosted + `"}`), "", false},
		{"no mode", ptr(`{"dolt_server_host":"` + hosted + `"}`), "", false},
		{"loopback ip", ptr(`{"dolt_mode":"server","dolt_server_host":"127.0.0.1"}`), "", false},
		{"localhost", ptr(`{"dolt_mode":"server","dolt_server_host":"localhost"}`), "", false},
		{"ipv6 loopback", ptr(`{"dolt_mode":"server","dolt_server_host":"::1"}`), "", false},
		{"empty host", ptr(`{"dolt_mode":"server","dolt_server_host":""}`), "", false},
		{"missing host", ptr(`{"dolt_mode":"server"}`), "", false},
		{"missing file", nil, "", false},
		{"malformed", ptr(`{not json`), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.metadata != nil {
				dir := filepath.Join(home, ".beads")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(*tc.metadata), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			host, ok := HostedServer(home)
			if host != tc.wantHost || ok != tc.wantOK {
				t.Errorf("HostedServer = (%q, %v), want (%q, %v)", host, ok, tc.wantHost, tc.wantOK)
			}
		})
	}
}

func ptr(s string) *string { return &s }
