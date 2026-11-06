package verbs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const resumeNotice = "Dispatcher resumed this parked run. You have new mail — run `ateam mail inbox` to read it."

// wantInboxRing is the C11 script for a home and initiative, with both paths
// already single-quoted.
func wantInboxRing(quotedMailbox, quotedWake string) string {
	return "#!/bin/sh\n" +
		"# Written by ateam cloud-boot. Dispatcher runs this after it writes $DSP_SUP/inbox/<seq>.json.\n" +
		"mkdir -p " + quotedMailbox + " && touch " + quotedWake + "\n"
}

func TestCloudBootWritesInboxRingOnStartAndResume(t *testing.T) {
	for _, mode := range []string{"start", "resume"} {
		t.Run(mode, func(t *testing.T) {
			e := newCBEnv(t)
			e.desc = "repo: x\nsession: sid-1\n"
			e.common.IdlePolls = 1
			e.leadSeq("sid-1", "idle")
			var err error
			if mode == "start" {
				err = e.start()
			} else {
				err = e.resume()
			}
			wantExit(t, err, cloudBootParkExit)

			path := filepath.Join(e.sup, "bin", "inbox-ring")
			fi, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if fi.Mode().Perm() != 0o755 {
				t.Errorf("mode = %v, want 0755", fi.Mode().Perm())
			}
			data, _ := os.ReadFile(path)
			mailbox := filepath.Join(e.ctx.Home, "mailbox")
			want := wantInboxRing("'"+mailbox+"'", "'"+filepath.Join(mailbox, "at-ab12.wake")+"'")
			if string(data) != want {
				t.Errorf("script:\n got %q\nwant %q", data, want)
			}
			if fi, err := os.Stat(filepath.Join(e.sup, "inbox")); err != nil || !fi.IsDir() {
				t.Errorf("inbox dir missing: %v", err)
			}

			if out, err := exec.Command(path).CombinedOutput(); err != nil {
				t.Fatalf("run script: %v: %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(mailbox, "at-ab12.wake")); err != nil {
				t.Errorf("doorbell not touched: %v", err)
			}
		})
	}
}

func TestWriteInboxRingQuotesSingleQuoteInHome(t *testing.T) {
	sup := t.TempDir()
	home := filepath.Join(t.TempDir(), "it's home")
	if err := writeInboxRing(home, sup, "at-ab12"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sup, "bin", "inbox-ring")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `'\''`) {
		t.Errorf("single quote not escaped:\n%s", data)
	}
	if out, err := exec.Command(path).CombinedOutput(); err != nil {
		t.Fatalf("run script: %v: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "mailbox", "at-ab12.wake")); err != nil {
		t.Errorf("doorbell not touched under quoted home: %v", err)
	}
}

func TestWriteInboxRingRewritesExistingScriptMode(t *testing.T) {
	sup := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sup, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sup, "bin", "inbox-ring")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeInboxRing(t.TempDir(), sup, "at-ab12"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", fi.Mode().Perm())
	}
}

func TestCloudBootResumeNotice(t *testing.T) {
	cases := []struct {
		name       string
		files      map[string]string // relative to sup
		wantNotice bool
	}{
		{"inbox file present", map[string]string{"inbox/48213.json": "{}"}, true},
		{"empty inbox", nil, false},
		{"only read files", map[string]string{"inbox/read/48213.json": "{}"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newCBEnv(t)
			e.desc = "repo: x\nsession: sid-1\n"
			e.common.IdlePolls = 1
			e.leadSeq("sid-1", "idle")
			for rel, content := range tc.files {
				p := filepath.Join(e.sup, rel)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			wantExit(t, e.resume(), cloudBootParkExit)
			prompt := e.tmux[0][len(e.tmux[0])-1]
			if tc.wantNotice {
				if prompt != resumeNotice {
					t.Errorf("prompt = %q, want the exact notice %q", prompt, resumeNotice)
				}
			} else if want := cloudWakePrompt("at-ab12", nil); prompt != want {
				t.Errorf("prompt = %q, want %q", prompt, want)
			}
		})
	}
}

func TestCloudBootStartIgnoresInboxForPrompt(t *testing.T) {
	e := newCBEnv(t)
	e.desc = "repo: x\n"
	e.common.IdlePolls = 1
	e.leadSeq("sid-1", "idle")
	if err := os.MkdirAll(filepath.Join(e.sup, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.sup, "inbox", "7.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantExit(t, e.start(), cloudBootParkExit)
	if prompt := e.tmux[0][len(e.tmux[0])-1]; prompt != "/dri at-ab12" {
		t.Errorf("prompt = %q, want /dri at-ab12", prompt)
	}
}
