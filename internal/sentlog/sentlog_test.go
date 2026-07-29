package sentlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
		ChatRef:    "555111234:98",
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

// sentConcurrentWriters is how many goroutines TestAppendSurvivesConcurrent
// Writers releases at once. It is a DETECTION-RATE parameter, not a taste
// one: the flaw it hunts is a gap between two write(2) calls, and the chance
// some other writer lands in that gap rises with the number of writers
// racing for it. Measured against the two-write mutation this test exists to
// catch, 256 writers caught it on 30 of 30 runs; the value is not arbitrary.
const sentConcurrentWriters = 256

// sentConcurrentBodyBytes sizes each record's body. Big enough that records
// are distinguishable and a merged line is unmistakable, small enough that
// os.File.Write never has to loop over a short write — which would split the
// record into two write(2) calls in the TEST's own setup and defeat the
// property being measured.
const sentConcurrentBodyBytes = 512

// TestAppendSurvivesConcurrentWriters pins contract §2.3's NON-NEGOTIABLE
// one-write-per-record discipline: "The append is ONE f.Write(line) call on
// an O_APPEND fd — this is what makes concurrent writers safe without a lock.
// Do not introduce a bufio.Writer, a second Write for the newline, or an
// fmt.Fprintf."
//
// WHY THIS SHAPE, AND WHY NO ASSERTION ON FILE CONTENT ALONE: single-
// threaded, one Write and two Writes produce byte-identical files. Every
// other test in this package asserts on the resulting content, so all of them
// pass against a split write — measured, and the reason this bead exists.
// The discipline is only observable when writers actually race, because that
// is the only condition under which it does anything: POSIX makes the
// seek-and-write of a single write(2) on an O_APPEND fd atomic, so one write
// per record is what stops two concurrent appenders from interleaving.
//
// The real concurrency is not hypothetical — every short-lived `ateam` CLI
// process appends here, alongside the relay's 5-minute hung-ticker goroutine
// (internal/verbs/hung_tick.go). Under a split write, a second writer's
// record lands between a record and its newline, turning two good records
// into two malformed lines. `ateam sent` skips malformed lines with a stderr
// warning, so the visible symptom is TWO RECORDS SILENTLY MISSING from the
// audit trail — the exact failure this log exists to prevent.
//
// A failure here is therefore never "flaky": correct code cannot interleave,
// so a corrupt line means the discipline was broken.
func TestAppendSurvivesConcurrentWriters(t *testing.T) {
	home := t.TempDir()

	// Force real parallelism even on a single-core runner, so the window
	// between a hypothetical pair of writes is genuinely contended rather
	// than serialised away by the scheduler.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(sentConcurrentWriters))

	// bodyFor makes each record's body unique AND self-identifying, so a
	// line holding two spliced records is detectable even if it somehow
	// parsed as JSON.
	bodyFor := func(i int) string {
		tag := fmt.Sprintf("<%04d>", i)
		return strings.Repeat(tag, sentConcurrentBodyBytes/len(tag))
	}

	// All writers block on the same barrier and are released together;
	// staggered starts would serialise the very contention being measured.
	var barrier sync.WaitGroup
	barrier.Add(1)
	var done sync.WaitGroup
	errs := make([]error, sentConcurrentWriters)

	for i := 0; i < sentConcurrentWriters; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			rec := Record{
				Timestamp:  "2026-07-24T18:00:00Z",
				Sender:     KindNotify,
				Transport:  "telegram",
				Initiative: fmt.Sprintf("at-%04d", i),
				Body:       bodyFor(i),
				Outcome:    OutcomeSent,
			}
			barrier.Wait()
			errs[i] = Append(home, rec)
		}(i)
	}
	barrier.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Append from writer %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(Path(home))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// The file must end with exactly one newline and nothing after it: a
	// record whose newline was written separately can leave a stray empty
	// line or an unterminated tail.
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("log does not end in a newline — a record was left unterminated")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")

	if len(lines) != sentConcurrentWriters {
		t.Fatalf("%d writers each appending ONE record produced %d lines — "+
			"records interleaved, so Append is not one write(2) per record "+
			"(contract §2.3)", sentConcurrentWriters, len(lines))
	}

	// Every line must be a whole record, and the set of records recovered
	// must be exactly the set written — no duplicates, no losses. Checked by
	// initiative id, which is unique per writer.
	seen := make(map[string]bool, sentConcurrentWriters)
	for n, line := range lines {
		if line == "" {
			t.Fatalf("line %d is empty — a newline was written separately from "+
				"its record (contract §2.3)", n)
		}
		var got Record
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line %d is not a whole JSON record: %v\nline begins: %.120q",
				n, err, line)
		}
		var i int
		if _, err := fmt.Sscanf(got.Initiative, "at-%04d", &i); err != nil {
			t.Fatalf("line %d has initiative %q, which no writer wrote", n, got.Initiative)
		}
		if seen[got.Initiative] {
			t.Fatalf("line %d repeats initiative %q — a record was written twice",
				n, got.Initiative)
		}
		seen[got.Initiative] = true
		// Body integrity: a splice that still parsed would carry the wrong
		// body for its initiative.
		if want := bodyFor(i); got.Body != want {
			t.Fatalf("line %d (%s) body is %d bytes, want %d — record spliced",
				n, got.Initiative, len(got.Body), len(want))
		}
	}
	if len(seen) != sentConcurrentWriters {
		t.Fatalf("recovered %d distinct records from %d writers", len(seen), sentConcurrentWriters)
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

// TestKindKnown verifies the seven real kinds report Known() and that neither
// the empty string nor KindUndeclared nor an arbitrary value does.
func TestKindKnown(t *testing.T) {
	for _, k := range []Kind{KindNotify, KindNotifyBriefing, KindNotifyReviews, KindNotifyDirect, KindDispatch, KindClose, KindRelayHung} {
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

// TestRedactErrorStripsURLCredentials verifies RedactError reduces an
// embedded URL to scheme://host (dropping a bot-token-shaped path, plus any
// userinfo/query), leaves a plain non-URL error message untouched, and
// returns "" for a nil error.
func TestRedactErrorStripsURLCredentials(t *testing.T) {
	if got := RedactError(nil); got != "" {
		t.Errorf("RedactError(nil) = %q, want \"\"", got)
	}

	plain := errors.New("boom")
	if got := RedactError(plain); got != "boom" {
		t.Errorf("RedactError(plain) = %q, want %q (no URL, untouched)", got, "boom")
	}

	const token = "123456:AAFsecretBotTokenValueXYZ"
	urlErr := errors.New(`Post "https://api.telegram.org/bot` + token + `/sendMessage": dial tcp: connection refused`)
	got := RedactError(urlErr)
	if strings.Contains(got, token) {
		t.Fatalf("RedactError leaked the token: %q", got)
	}
	if !strings.Contains(got, "https://api.telegram.org") {
		t.Fatalf("RedactError dropped the host, want it preserved: %q", got)
	}
	if strings.Contains(got, "/sendMessage") {
		t.Fatalf("RedactError should drop the path entirely, got: %q", got)
	}
}
