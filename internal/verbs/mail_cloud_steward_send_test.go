package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
)

// stewardSendEnv extends cloudSendEnv with fakes for the steward create path.
type stewardSendEnv struct {
	*cloudSendEnv
	listArgs   []string
	listOut    string
	createArgs []string // dir, actor, title, key
	createBody string
	createCall int
	createID   string
	createErr  error
}

func newStewardSendEnv(t *testing.T) *stewardSendEnv {
	t.Helper()
	e := &stewardSendEnv{cloudSendEnv: newCloudSendEnv(t, true), listOut: "[]", createID: "eri-mail1"}
	cloudSendBD = func(h string) cli.BDRunner {
		e.cloudBDHomes = append(e.cloudBDHomes, h)
		return &fakeBD{runFn: func(args ...string) (string, error) {
			if h == e.dispatcherDir && args[0] == "list" {
				e.listArgs = args
				return e.listOut, nil
			}
			t.Errorf("unexpected bd call on %s: %v", h, args)
			return "", errors.New("unexpected")
		}}
	}
	old := cloudStewardCreate
	t.Cleanup(func() { cloudStewardCreate = old })
	e.enterStewardSession(t)
	cloudStewardCreate = func(dir, actor, title, key, bodyFile string) (string, error) {
		e.createCall++
		e.createArgs = []string{dir, actor, title, key}
		raw, err := os.ReadFile(bodyFile)
		if err != nil {
			t.Fatal(err)
		}
		e.createBody = string(raw)
		return e.createID, e.createErr
	}
	return e
}

// enterStewardSession writes the steward session marker and changes into its
// directory, so the send runs as the steward session.
func (e *stewardSendEnv) enterStewardSession(t *testing.T) {
	t.Helper()
	marker := sentlog.StewardMarkerPath(e.ctx.Home)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(marker))
}

func (e *stewardSendEnv) sendSteward(sender string) error {
	c := &sendKong{RecipientID: CloudStewardHandle, File: e.file}
	return c.sendToCloudSteward(e.ctx, sender)
}

func TestCloudStewardSendHappyPath(t *testing.T) {
	e := newStewardSendEnv(t)
	if err := e.sendSteward(StewardHandle); err != nil {
		t.Fatal(err)
	}
	key := laptopMailKey("testhost", StewardHandle, []byte("hello cloud"))
	want := []string{e.dispatcherDir, "laptop:steward", "local-steward: message", key}
	if strings.Join(e.createArgs, "|") != strings.Join(want, "|") {
		t.Errorf("create args = %v, want %v", e.createArgs, want)
	}
	if want := cloudStewardMailBody(LocalStewardHandle, "hello cloud", key); e.createBody != want {
		t.Errorf("body = %q, want %q", e.createBody, want)
	}
	if !strings.Contains(e.createBody, "kind: steward-message") {
		t.Errorf("body lacks kind line: %q", e.createBody)
	}
	if !strings.Contains(e.sqlQuery, "issue_id='eri-mail1'") || !strings.Contains(e.sqlQuery, "op='create'") || !strings.Contains(e.sqlQuery, "seq > 42") {
		t.Errorf("journal query = %q", e.sqlQuery)
	}
	if got := e.stdout.String(); !strings.Contains(got, "message_id: eri-mail1") || !strings.Contains(got, "sent to cloud-steward") {
		t.Errorf("stdout = %q", got)
	}
	wantList := "list --type=task --label=type:mail --label=mail-key:" + key + " --assignee=agent:steward --all --json"
	if got := strings.Join(e.listArgs, " "); got != wantList {
		t.Errorf("dedup list = %q, want %q", got, wantList)
	}
}

func TestCloudStewardCreateArgv(t *testing.T) {
	args := cloudStewardCreateArgs("/d", "laptop:steward", "local-steward: message", "laptop:h:abc", "/tmp/f")
	got := strings.Join(args, " ")
	want := "-C /d --actor laptop:steward create --type=task --labels type:mail,mail-key:laptop:h:abc --assignee=agent:steward --title local-steward: message --body-file /tmp/f --json"
	if got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
	if strings.Contains(got, "--type=mail") || strings.Contains(got, "--type mail") {
		t.Errorf("argv passes the mail type: %q", got)
	}
}

func TestCloudStewardSendDedup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		age     time.Duration
		deliver bool
	}{
		{"inside window", time.Minute, false},
		{"outside window", cloudDedupWindow + time.Minute, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newStewardSendEnv(t)
			out, _ := json.Marshal([]bd.Issue{{ID: "eri-old1", CreatedAt: testCloudNow.Add(-tc.age).Format(time.RFC3339)}})
			e.listOut = string(out)
			if err := e.sendSteward(StewardHandle); err != nil {
				t.Fatal(err)
			}
			if got := e.createCall == 1; got != tc.deliver {
				t.Errorf("created = %t, want %t", got, tc.deliver)
			}
			if !tc.deliver && !strings.Contains(e.stdout.String(), "already delivered to cloud-steward as eri-old1") {
				t.Errorf("stdout = %q", e.stdout.String())
			}
		})
	}
}

func TestCloudStewardSendRefusals(t *testing.T) {
	t.Run("non-steward sender", func(t *testing.T) {
		e := newStewardSendEnv(t)
		err := e.sendSteward("at-me1")
		if err == nil || !strings.Contains(err.Error(), "only the laptop steward can mail cloud-steward") {
			t.Fatalf("err = %v", err)
		}
		if e.createCall != 0 {
			t.Error("created a bead")
		}
	})
	t.Run("steward sender from a non-steward session", func(t *testing.T) {
		e := newStewardSendEnv(t)
		t.Chdir(t.TempDir())
		err := e.sendSteward(StewardHandle)
		if err == nil || !strings.Contains(err.Error(), "only the laptop steward can mail cloud-steward") {
			t.Fatalf("err = %v", err)
		}
		if e.createCall != 0 {
			t.Error("created a bead")
		}
	})
	t.Run("hosted", func(t *testing.T) {
		e := newStewardSendEnv(t)
		meta := filepath.Join(e.ctx.Home, ".beads")
		if err := os.MkdirAll(meta, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(meta, "metadata.json"),
			[]byte(`{"dolt_mode":"server","dolt_server_host":"db.example.com"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := e.sendSteward(StewardHandle); err == nil || !strings.Contains(err.Error(), "hosted") {
			t.Fatalf("err = %v", err)
		}
		if e.createCall != 0 {
			t.Error("created a bead")
		}
	})
	t.Run("missing dispatcher_beads", func(t *testing.T) {
		e := newStewardSendEnv(t)
		cfg := "cloud_home = \"" + e.cloudHome + "\"\n"
		if err := os.WriteFile(filepath.Join(e.ctx.Home, "config.toml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		err := e.sendSteward(StewardHandle)
		var ue *cli.UsageError
		if err == nil || !strings.Contains(err.Error(), "dispatcher_beads") || !errors.As(err, &ue) {
			t.Fatalf("err = %v, want usage error naming dispatcher_beads", err)
		}
	})
	t.Run("missing cloud_home", func(t *testing.T) {
		e := newStewardSendEnv(t)
		cfg := "dispatcher_beads = \"" + e.dispatcherDir + "\"\n"
		if err := os.WriteFile(filepath.Join(e.ctx.Home, "config.toml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := e.sendSteward(StewardHandle); err == nil || !strings.Contains(err.Error(), "cloud_home") {
			t.Fatalf("err = %v", err)
		}
		if e.createCall != 0 {
			t.Error("created a bead")
		}
	})
	t.Run("empty body", func(t *testing.T) {
		e := newStewardSendEnv(t)
		if err := os.WriteFile(e.file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := e.sendSteward(StewardHandle); err == nil || !strings.Contains(err.Error(), "is empty") {
			t.Fatalf("err = %v", err)
		}
		if e.createCall != 0 {
			t.Error("created a bead")
		}
	})
}

func TestCloudStewardSendNoJournalRowErrors(t *testing.T) {
	e := newStewardSendEnv(t)
	e.sqlOut = "[]"
	err := e.sendSteward(StewardHandle)
	if err == nil || !strings.Contains(err.Error(), "created eri-mail1 without an events-journal row; Dispatcher will not wake the steward") {
		t.Fatalf("err = %v", err)
	}
}

func TestCloudStewardSendHeadReadFailureWarns(t *testing.T) {
	e := newStewardSendEnv(t)
	e.headErr = errors.New("boom")
	if err := e.sendSteward(StewardHandle); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.stderr.String(), "warning") || !strings.Contains(e.stdout.String(), "message_id: eri-mail1") {
		t.Errorf("stderr=%q stdout=%q", e.stderr.String(), e.stdout.String())
	}
}

func TestCloudStewardSendCreateFailure(t *testing.T) {
	e := newStewardSendEnv(t)
	e.createErr = errors.New("bd down")
	err := e.sendSteward(StewardHandle)
	if err == nil || !strings.Contains(err.Error(), "nothing sent") {
		t.Fatalf("err = %v", err)
	}
}
