package sentlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMarshalLineRoundTrip verifies MarshalLine produces one JSON object
// followed by exactly one trailing newline, and that the object round-trips
// through json.Unmarshal back to an equal Record.
func TestMarshalLineRoundTrip(t *testing.T) {
	rec := Record{
		Timestamp:  "2026-07-24T18:03:11Z",
		Sender:     KindNotify,
		Transport:  "telegram",
		Initiative: "at-atnl",
		ThreadRef:  "412",
		General:    false,
		Title:      "Architecture Review",
		Body:       "please review",
		Outcome:    OutcomeSent,
		Error:      "",
		SessionID:  "abc-123",
		Cwd:        "/Users/erlloyd/work",
		StewardCwd: false,
		PID:        12345,
	}

	line, err := rec.MarshalLine()
	if err != nil {
		t.Fatalf("MarshalLine: %v", err)
	}
	if !strings.HasSuffix(string(line), "\n") {
		t.Fatalf("MarshalLine: expected trailing newline, got %q", line)
	}
	if strings.Count(string(line), "\n") != 1 {
		t.Fatalf("MarshalLine: expected exactly one newline, got %q", line)
	}

	var got Record
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != rec {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, rec)
	}
}

// TestAppendOneLinePerRecord verifies Append writes exactly one line per
// call, creating the parent directory and file when neither exists yet
// (missing-file readback), and that the file grows by one line per Append.
func TestAppendOneLinePerRecord(t *testing.T) {
	home := t.TempDir()

	path := Path(home)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no log file yet, stat err = %v", err)
	}

	rec1 := Record{Timestamp: "2026-07-24T18:00:00Z", Sender: KindNotify, Outcome: OutcomeSent}
	if err := Append(home, rec1); err != nil {
		t.Fatalf("Append (create): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after first Append: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line after first Append, got %d: %q", len(lines), data)
	}
	var got1 Record
	if err := json.Unmarshal([]byte(lines[0]), &got1); err != nil {
		t.Fatalf("unmarshal first line: %v", err)
	}
	if got1.Sender != KindNotify {
		t.Fatalf("first line sender = %q, want %q", got1.Sender, KindNotify)
	}

	rec2 := Record{Timestamp: "2026-07-24T18:05:00Z", Sender: KindClose, Outcome: OutcomeFailed, Error: "boom"}
	if err := Append(home, rec2); err != nil {
		t.Fatalf("Append (second): %v", err)
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after second Append: %v", err)
	}
	lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after second Append, got %d: %q", len(lines), data)
	}
	var got2 Record
	if err := json.Unmarshal([]byte(lines[1]), &got2); err != nil {
		t.Fatalf("unmarshal second line: %v", err)
	}
	if got2.Sender != KindClose || got2.Outcome != OutcomeFailed || got2.Error != "boom" {
		t.Fatalf("second line = %+v, unexpected", got2)
	}
}

// TestAppendCreatesParentDir verifies Append MkdirAll's a home directory
// that does not exist yet, rather than failing.
func TestAppendCreatesParentDir(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-yet-created", "nested")
	if err := Append(home, Record{Timestamp: "2026-07-24T18:00:00Z", Sender: KindDispatch}); err != nil {
		t.Fatalf("Append into non-existent home: %v", err)
	}
	if _, err := os.Stat(Path(home)); err != nil {
		t.Fatalf("expected log file to exist after Append: %v", err)
	}
}

// TestDeriveZeroValueDegradation verifies Derive never fails and degrades
// every derived field to its zero value under the documented conditions:
// no CLAUDE_CODE_SESSION_ID set, the "unknown" sentinel, and no steward
// marker present.
func TestDeriveZeroValueDegradation(t *testing.T) {
	home := t.TempDir()

	t.Run("no session id set", func(t *testing.T) {
		orig, had := os.LookupEnv(sessionIDEnvVar)
		os.Unsetenv(sessionIDEnvVar)
		if had {
			defer os.Setenv(sessionIDEnvVar, orig)
		}
		rec := Derive(home)
		if rec.SessionID != "" {
			t.Errorf("SessionID = %q, want \"\"", rec.SessionID)
		}
	})

	t.Run("unknown sentinel treated as absent", func(t *testing.T) {
		t.Setenv(sessionIDEnvVar, "unknown")
		rec := Derive(home)
		if rec.SessionID != "" {
			t.Errorf("SessionID = %q, want \"\" for the unknown sentinel", rec.SessionID)
		}
	})

	t.Run("real session id passes through", func(t *testing.T) {
		t.Setenv(sessionIDEnvVar, "sess-42")
		rec := Derive(home)
		if rec.SessionID != "sess-42" {
			t.Errorf("SessionID = %q, want %q", rec.SessionID, "sess-42")
		}
	})

	t.Run("no steward marker present", func(t *testing.T) {
		rec := Derive(home)
		if rec.StewardCwd {
			t.Errorf("StewardCwd = true, want false when no marker file exists")
		}
		if rec.Cwd == "" {
			t.Errorf("Cwd = \"\", want a resolvable cwd")
		}
		if rec.PID == 0 {
			t.Errorf("PID = 0, want os.Getpid()")
		}
	})
}

// TestKindKnown verifies the six real kinds report Known() and that neither
// the empty string nor KindUndeclared nor an arbitrary value does.
func TestKindKnown(t *testing.T) {
	for _, k := range []Kind{KindNotify, KindNotifyBriefing, KindNotifyDirect, KindDispatch, KindClose, KindRelayHung} {
		if !k.Known() {
			t.Errorf("Kind(%q).Known() = false, want true", k)
		}
	}
	for _, k := range []Kind{"", KindUndeclared, "bogus"} {
		if k.Known() {
			t.Errorf("Kind(%q).Known() = true, want false", k)
		}
	}
}
