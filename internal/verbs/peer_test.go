package verbs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// peerStore is an in-memory bd store: initiatives by id plus the message beads
// `bd create` makes. It backs both the laptop workspace and cloud_home.
type peerStore struct {
	t          *testing.T
	issues     map[string]*bd.Issue
	updates    []string        // ids passed to `bd update`, in order
	dropUpdate map[string]bool // ids whose update is accepted but not applied
	msgs       []bd.Issue
}

func newPeerStore(t *testing.T, issues ...bd.Issue) *peerStore {
	s := &peerStore{t: t, issues: map[string]*bd.Issue{}, dropUpdate: map[string]bool{}}
	for i := range issues {
		iss := issues[i]
		s.issues[iss.ID] = &iss
	}
	return s
}

func (s *peerStore) runner() *fakeBD {
	return &fakeBD{
		runFn: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				iss, ok := s.issues[args[1]]
				if !ok {
					return "[]", nil
				}
				return issueJSON(*iss), nil
			case "update":
				id := args[1]
				for _, a := range args {
					path, ok := strings.CutPrefix(a, "--body-file=")
					if !ok {
						continue
					}
					raw, err := os.ReadFile(path)
					if err != nil {
						s.t.Fatal(err)
					}
					s.updates = append(s.updates, id)
					if !s.dropUpdate[id] {
						s.issues[id].Description = string(raw)
					}
				}
			}
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			switch args[0] {
			case "create":
				msg := bd.Issue{ID: "msg-" + string(rune('a'+len(s.msgs))), IssueType: "message", Status: "open"}
				for _, a := range args {
					if v, ok := strings.CutPrefix(a, "--assignee="); ok {
						msg.Assignee = v
					}
					if v, ok := strings.CutPrefix(a, "--labels="); ok {
						msg.Labels = append(msg.Labels, v)
					}
					if v, ok := strings.CutPrefix(a, "--body-file="); ok {
						raw, err := os.ReadFile(v)
						if err != nil {
							s.t.Fatal(err)
						}
						msg.Description = string(raw)
					}
				}
				s.msgs = append(s.msgs, msg)
				*dst.(*bd.Issue) = msg
			case "list":
				var out []bd.Issue
				for _, m := range s.msgs {
					match := true
					for _, a := range args {
						if v, ok := strings.CutPrefix(a, "--assignee="); ok && m.Assignee != v {
							match = false
						}
						if v, ok := strings.CutPrefix(a, "--label="); ok {
							found := false
							for _, l := range m.Labels {
								found = found || l == v
							}
							match = match && found
						}
					}
					if match {
						out = append(out, m)
					}
				}
				*dst.(*[]bd.Issue) = out
			}
			return nil
		},
	}
}

// peerEnv wires a laptop store, a cloud_home store, the Dispatcher run bead,
// and every delivery seam the notify path touches.
type peerEnv struct {
	ctx      *cli.Context
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	local    *peerStore
	cloud    *peerStore
	comments []string // run-bead comment texts
	cloudDir string
	dispDir  string
	resumed  []string
	sendErr  error // returned by the fake resume, to fail a local notify
}

const (
	peerLocalA = "at-lap1"
	peerLocalB = "at-lap2"
	peerCloud  = "at-cld1"
	peerRun    = "eri-run9"
)

func newPeerEnv(t *testing.T, configure bool) *peerEnv {
	t.Helper()
	e := &peerEnv{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, cloudDir: t.TempDir(), dispDir: t.TempDir()}
	t.Setenv("AGENT_TEAMS_HOST", "testhost")
	wt := t.TempDir()
	e.local = newPeerStore(t,
		bd.Issue{ID: peerLocalA, Title: "Laptop A", Status: "open", Description: "worktree: " + wt},
		bd.Issue{ID: peerLocalB, Title: "Laptop B", Status: "open", Description: "worktree: " + wt},
	)
	e.cloud = newPeerStore(t,
		bd.Issue{ID: peerCloud, Title: "Cloud one", Status: "open", Description: "where: run:" + peerRun},
	)
	home := t.TempDir()
	if configure {
		cfg := "cloud_home = \"" + e.cloudDir + "\"\ndispatcher_beads = \"" + e.dispDir + "\"\n"
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.ctx = &cli.Context{Home: home, BD: e.local.runner(), Stdout: e.stdout, Stderr: e.stderr}

	oldBD, oldComment, oldSQL, oldNow, oldSend := cloudSendBD, cloudSendComment, cloudSendSQL, cloudSendNow, newPeerSend
	t.Cleanup(func() {
		cloudSendBD, cloudSendComment, cloudSendSQL, cloudSendNow, newPeerSend = oldBD, oldComment, oldSQL, oldNow, oldSend
	})
	cloudSendNow = func() time.Time { return testCloudNow }
	cloudSendBD = func(h string) cli.BDRunner {
		switch h {
		case e.cloudDir:
			return e.cloud.runner()
		case e.dispDir:
			return &fakeBD{runFn: func(args ...string) (string, error) {
				switch args[0] {
				case "show":
					return issueJSON(bd.Issue{ID: peerRun, Status: "open", Description: "meta-initiative: " + peerCloud + "\n---\nx"}), nil
				case "comments":
					return commentsJSON(e.comments), nil
				}
				return "", errors.New("unexpected dispatcher call: " + strings.Join(args, " "))
			}}
		}
		t.Errorf("unexpected store %s", h)
		return &fakeBD{}
	}
	cloudSendComment = func(dir, actor, runBead, file string) error {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		e.comments = append(e.comments, string(raw))
		return nil
	}
	cloudSendSQL = func(dir, query string) (string, error) {
		if query == journalHeadQuery {
			return `[{"head": 1}]`, nil
		}
		return `[{"seq": 2}]`, nil
	}
	newPeerSend = func() sendKong {
		s := newSendKong()
		s.agentsFunc = func() ([]agentSession, error) { return nil, nil }
		s.resumeFunc = func(_ *cli.Context, id, _, _ string) error {
			e.resumed = append(e.resumed, id)
			return e.sendErr
		}
		s.sleeper = func(time.Duration) {}
		s.doorbellExists = func(string) bool { return false }
		s.respawnFunc = func(string) error { return nil }
		return s
	}
	return e
}

// commentsJSON renders `bd comments --json` output for texts, all recent.
func commentsJSON(texts []string) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, tx := range texts {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"text":` + jsonQuote(tx) + `,"created_at":"` + testCloudNow.Format(time.RFC3339) + `"}`)
	}
	sb.WriteString("]")
	return sb.String()
}

func jsonQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

func (e *peerEnv) link(a, b string) error {
	return (&peerLinkKong{A: a, B: b}).Run(e.ctx)
}

func peersOf(s *peerStore, id string) []string {
	return initiative.Of(*s.issues[id]).Peers
}

func TestPeerLinkLaptopAndCloud(t *testing.T) {
	e := newPeerEnv(t, true)
	if err := e.link(peerLocalA, peerCloud); err != nil {
		t.Fatalf("link: %v\nstdout: %s\nstderr: %s", err, e.stdout, e.stderr)
	}
	if got := peersOf(e.local, peerLocalA); len(got) != 1 || got[0] != peerCloud {
		t.Errorf("laptop peers = %v, want [%s]", got, peerCloud)
	}
	if got := peersOf(e.cloud, peerCloud); len(got) != 1 || got[0] != peerLocalA {
		t.Errorf("cloud peers = %v, want [%s]", got, peerLocalA)
	}
	out := e.stdout.String()
	for _, want := range []string{
		"peer link: recorded peer: " + peerCloud + " on " + peerLocalA + " (laptop)\n",
		"peer link: recorded peer: " + peerLocalA + " on " + peerCloud + " (cloud)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}

	wantLocal := peerLinkMessage(
		peerSide{ID: peerLocalA, Kind: "laptop"},
		peerSide{ID: peerCloud, Kind: "cloud", Issue: bd.Issue{Title: "Cloud one"}})
	if !strings.HasPrefix(wantLocal, "PEER LINK: at-lap1 is paired with at-cld1 (cloud initiative: Cloud one).\nIt is listed as a peer: line in ateam show at-lap1.\n") {
		t.Errorf("frozen text drifted:\n%s", wantLocal)
	}
	if len(e.local.msgs) != 1 {
		t.Fatalf("local messages = %d, want 1", len(e.local.msgs))
	}
	if m := e.local.msgs[0]; m.Assignee != peerLocalA || m.Description != wantLocal ||
		!containsLabel(m.Labels, "dedup:peer-link-"+peerCloud) {
		t.Errorf("local message = %+v, want assignee %s, frozen text, dedup label", m, peerLocalA)
	}

	wantCloud := peerLinkMessage(
		peerSide{ID: peerCloud, Kind: "cloud"},
		peerSide{ID: peerLocalA, Kind: "laptop", Issue: bd.Issue{Title: "Laptop A"}})
	if len(e.comments) != 1 || !strings.Contains(e.comments[0], wantCloud) {
		t.Errorf("run-bead comments = %q, want one carrying %q", e.comments, wantCloud)
	}
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestPeerLinkRerunIsIdempotentAndDedupes(t *testing.T) {
	e := newPeerEnv(t, true)
	if err := e.link(peerLocalA, peerCloud); err != nil {
		t.Fatal(err)
	}
	updates := len(e.local.updates) + len(e.cloud.updates)
	e.stdout.Reset()
	if err := e.link(peerLocalA, peerCloud); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if got := len(e.local.updates) + len(e.cloud.updates); got != updates {
		t.Errorf("re-run wrote %d descriptions, want 0", got-updates)
	}
	out := e.stdout.String()
	for _, want := range []string{
		"peer link: " + peerLocalA + " already lists peer: " + peerCloud + "\n",
		"peer link: " + peerCloud + " already lists peer: " + peerLocalA + "\n",
		"(deduplicated)",
		"already delivered to cloud initiative " + peerCloud,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}
	if len(e.local.msgs) != 1 || len(e.comments) != 1 {
		t.Errorf("re-run sent more: local msgs %d, comments %d, want 1 each", len(e.local.msgs), len(e.comments))
	}
}

func TestPeerLinkCloudWithoutRunSkipsNotify(t *testing.T) {
	e := newPeerEnv(t, true)
	e.cloud.issues[peerCloud].Description = "problem text"
	if err := e.link(peerLocalA, peerCloud); err != nil {
		t.Fatalf("link: %v\n%s", err, e.stderr)
	}
	want := "peer link: " + peerCloud + " has no cloud run yet; it reads its peer: lines from ateam show when it boots\n"
	if !strings.Contains(e.stdout.String(), want) {
		t.Errorf("stdout missing %q:\n%s", want, e.stdout)
	}
	if len(e.comments) != 0 {
		t.Errorf("comments = %d, want none", len(e.comments))
	}
	if len(peersOf(e.cloud, peerCloud)) != 1 {
		t.Errorf("cloud side did not record the peer line")
	}
}

func TestPeerLinkRefusalsWriteNothing(t *testing.T) {
	e := newPeerEnv(t, true)
	e.local.issues["at-done"] = &bd.Issue{ID: "at-done", Status: "closed"}
	cases := []struct {
		name, a, b, want string
	}{
		{"same id", peerLocalA, peerLocalA, "are the same initiative; nothing written"},
		{"bad shape", peerLocalA, "NOPE", `"NOPE" is not a valid initiative id; nothing written`},
		{"unknown", peerLocalA, "at-zzz9", "initiative at-zzz9 not found in this workspace or in cloud_home (" + e.cloudDir + "); nothing written"},
		{"closed", peerLocalA, "at-done", "ateam peer link: at-done is closed; nothing written"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := e.link(tc.a, tc.b)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			if n := len(e.local.updates) + len(e.cloud.updates) + len(e.local.msgs) + len(e.comments); n != 0 {
				t.Errorf("wrote %d things, want 0", n)
			}
		})
	}

	t.Run("no cloud_home", func(t *testing.T) {
		e := newPeerEnv(t, false)
		err := e.link(peerLocalA, "at-zzz9")
		want := "ateam peer link: initiative at-zzz9 not found in this workspace; nothing written"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
	})

	t.Run("hosted home", func(t *testing.T) {
		e := newPeerEnv(t, true)
		hosted := filepath.Join(e.ctx.Home, ".beads", "metadata.json")
		if err := os.MkdirAll(filepath.Dir(hosted), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(hosted, []byte(`{"dolt_mode":"server","dolt_server_host":"db.example.com"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := bd.HostedServer(e.ctx.Home); !ok {
			t.Skip("fixture does not make this home hosted")
		}
		err := e.link(peerLocalA, peerLocalB)
		want := "ateam peer link: run this on a laptop; a cloud run cannot write a laptop initiative; nothing written"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
		if len(e.local.updates) != 0 {
			t.Errorf("wrote on a hosted home")
		}
	})
}

func TestPeerLinkReadAfterWriteCatchesDroppedLine(t *testing.T) {
	e := newPeerEnv(t, true)
	e.cloud.dropUpdate[peerCloud] = true
	err := e.link(peerLocalA, peerCloud)
	want := "ateam peer link: wrote peer: " + peerLocalA + " on " + peerCloud +
		", but a re-read does not show it (a concurrent description write may have replaced it); re-run: ateam peer link " +
		peerLocalA + " " + peerCloud
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if len(e.local.msgs) != 0 || len(e.comments) != 0 {
		t.Errorf("notified after a failed confirm")
	}
}

func TestPeerLinkLaptopAndLaptop(t *testing.T) {
	e := newPeerEnv(t, false)
	if err := e.link(peerLocalA, peerLocalB); err != nil {
		t.Fatalf("link: %v\n%s", err, e.stderr)
	}
	if got := peersOf(e.local, peerLocalA); len(got) != 1 || got[0] != peerLocalB {
		t.Errorf("A peers = %v", got)
	}
	if got := peersOf(e.local, peerLocalB); len(got) != 1 || got[0] != peerLocalA {
		t.Errorf("B peers = %v", got)
	}
	if len(e.local.msgs) != 2 {
		t.Errorf("messages = %d, want 2", len(e.local.msgs))
	}
}

func TestPeerLinkNotifyFailureWarnsAndExitsNonZero(t *testing.T) {
	e := newPeerEnv(t, false)
	e.sendErr = errors.New("resume failed")
	err := e.link(peerLocalA, peerLocalB)
	if err == nil {
		t.Fatal("want a non-nil error when a notify fails")
	}
	if !strings.Contains(e.stderr.String(), "peer link: warning: could not notify "+peerLocalA+": ") {
		t.Errorf("stderr = %q", e.stderr)
	}
	if len(peersOf(e.local, peerLocalA)) != 1 || len(peersOf(e.local, peerLocalB)) != 1 {
		t.Errorf("peer lines should be recorded despite the notify failure")
	}
}
