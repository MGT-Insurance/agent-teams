package verbs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func writeCloudHomeConfig(t *testing.T, home, cloud string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "cloud_home = \"" + cloud + "\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeCloudHomeUnsetOrSameWorkspaceIsOff(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		home := t.TempDir()
		got, ok, err := bridgeCloudHome(home)
		if err != nil || ok || got != "" {
			t.Errorf("got (%q, %t, %v), want (\"\", false, nil)", got, ok, err)
		}
	})
	t.Run("same workspace", func(t *testing.T) {
		home := t.TempDir()
		writeCloudHomeConfig(t, home, home)
		got, ok, err := bridgeCloudHome(home)
		if err != nil || ok || got != "" {
			t.Errorf("got (%q, %t, %v), want (\"\", false, nil)", got, ok, err)
		}
	})
	t.Run("set to another workspace", func(t *testing.T) {
		home, cloud := t.TempDir(), t.TempDir()
		writeCloudHomeConfig(t, home, cloud)
		got, ok, err := bridgeCloudHome(home)
		if err != nil || !ok || got != cloud {
			t.Errorf("got (%q, %t, %v), want (%q, true, nil)", got, ok, err, cloud)
		}
	})
}

func TestCloudCommentAuthorNeverWakeFiltered(t *testing.T) {
	cases := []struct{ sender, want string }{
		{"steward", "laptop:steward"},
		{"human", "laptop:human"},
		{"Eric Lloyd", "laptop:Eric Lloyd"},
		{"at-abc12", "initiative:at-abc12"},
		{"at-lnbg9", "initiative:at-lnbg9"},
	}
	for _, tc := range cases {
		got := cloudCommentAuthor(tc.sender)
		if got != tc.want {
			t.Errorf("cloudCommentAuthor(%q) = %q, want %q", tc.sender, got, tc.want)
		}
		if strings.HasPrefix(got, "job:") || strings.HasPrefix(got, "system:") || got == "reconcile" {
			t.Errorf("cloudCommentAuthor(%q) = %q is filtered by Dispatcher's wake path", tc.sender, got)
		}
	}
	// Even a sender spelled like a filtered author gets a prefix.
	for _, s := range []string{"job:x", "system:x", "reconcile"} {
		if got := cloudCommentAuthor(s); got == s || strings.HasPrefix(got, "job:") || strings.HasPrefix(got, "system:") {
			t.Errorf("cloudCommentAuthor(%q) = %q", s, got)
		}
	}
}

func TestCloudCommentTextMatchesDispatcherPeerFormat(t *testing.T) {
	got := cloudCommentText("at-abc12", "hello\nworld", "laptop:h:0123456789ab")
	want := "message from at-abc12:\nhello\nworld\n\nReply with: ateam mail send at-abc12 --file <file>\nmail-key: laptop:h:0123456789ab"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	steward := cloudCommentText("steward", "b", "k")
	if !strings.Contains(steward, "ateam mail send local-steward --file <file>") {
		t.Errorf("steward reply handle not mapped: %q", steward)
	}
}

func TestCloudReplyHandleMapsSteward(t *testing.T) {
	if got := cloudReplyHandle("steward"); got != LocalStewardHandle {
		t.Errorf("steward -> %q, want %q", got, LocalStewardHandle)
	}
	if got := cloudReplyHandle("at-abc12"); got != "at-abc12" {
		t.Errorf("at-abc12 -> %q", got)
	}
}

func TestLaptopMailKeyStable(t *testing.T) {
	k := laptopMailKey("mbp", "steward", []byte("body"))
	if k != laptopMailKey("mbp", "steward", []byte("body")) {
		t.Error("key is not deterministic")
	}
	if !regexp.MustCompile(`^laptop:mbp:[0-9a-f]{12}$`).MatchString(k) {
		t.Errorf("key shape = %q", k)
	}
	if k == laptopMailKey("mbp", "steward", []byte("body2")) || k == laptopMailKey("mbp", "human", []byte("body")) {
		t.Error("key does not vary with sender and body")
	}
	// sha256("steward\nbody") fixes the digest, so a change to the hashed input is caught.
	if want := "laptop:mbp:daa3636f02f0"; k != want {
		t.Errorf("key = %q, want %q", k, want)
	}
}

func TestBridgeDedupKey(t *testing.T) {
	if got := bridgeDedupKey("at-xyz"); got != "bridge-at-xyz" {
		t.Errorf("got %q", got)
	}
}
