package workspaceconfig

import (
	"strings"
	"testing"
)

func TestCloudDispatchKeys(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, "cloud_home = \"/h/hosted\"\ndispatcher_beads = \"/h/dsp\"\ndispatcher_actor = \"eric@example.com\"\n")
	for name, tc := range map[string]struct {
		read func(string) (string, bool, error)
		want string
	}{
		"cloud_home":       {CloudHome, "/h/hosted"},
		"dispatcher_beads": {DispatcherBeads, "/h/dsp"},
		"dispatcher_actor": {DispatcherActor, "eric@example.com"},
	} {
		got, ok, err := tc.read(home)
		if err != nil || !ok || got != tc.want {
			t.Errorf("%s = %q, %v, %v; want %q, true, nil", name, got, ok, err, tc.want)
		}
	}
}

func TestCloudDispatchKeysAbsent(t *testing.T) {
	home := t.TempDir()
	for _, read := range []func(string) (string, bool, error){CloudHome, DispatcherBeads, DispatcherActor} {
		if got, ok, err := read(home); err != nil || ok || got != "" {
			t.Errorf("absent key = %q, %v, %v; want empty, false, nil", got, ok, err)
		}
	}
}

func TestCloudDispatchKeysRejectEmptyAndUnknown(t *testing.T) {
	for _, key := range []string{"cloud_home", "dispatcher_beads", "dispatcher_actor"} {
		home := t.TempDir()
		writeConfig(t, home, key+" = \"\"\n")
		if _, _, err := CloudHome(home); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("empty %s: err = %v; want error naming the key", key, err)
		}
	}
	home := t.TempDir()
	writeConfig(t, home, "cloud_hom = \"x\"\n")
	if _, _, err := CloudHome(home); err == nil {
		t.Error("unknown key accepted")
	}
}
