// Core-path tests for `ateam sent` (contract agent-teams-48dh.1 §7): one
// per filter, one proving the filters AND together, truncation on and off,
// and the unparseable-timestamp skip branch. Edge cases beyond these belong
// to the tester, not here.
package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
)

// sentTS renders a timestamp d before now in the log's RFC3339 UTC form.
func sentTS(d time.Duration) string {
	return time.Now().Add(-d).UTC().Format(time.RFC3339)
}

// sentBodyAlphabet cycles through 62 distinct characters, none of them
// whitespace. Deliberately not a repeated character: a homogeneous fixture
// makes the position of a cut unobservable in the output, which lets a
// wrong cut point pass any containment-based assertion.
const sentBodyAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// sentBodySeq returns n runes of the alphabet cycle starting at index
// start, so the caller can name an exact slice of the body by position.
func sentBodySeq(start, n int) string {
	var b strings.Builder
	for i := start; i < start+n; i++ {
		b.WriteByte(sentBodyAlphabet[i%len(sentBodyAlphabet)])
	}
	return b.String()
}

// sentLine marshals rec the way the decorator writes it, without the
// trailing newline (sentWriteLog re-joins).
func sentLine(t *testing.T, rec sentlog.Record) string {
	t.Helper()
	line, err := rec.MarshalLine()
	if err != nil {
		t.Fatalf("MarshalLine: %v", err)
	}
	return strings.TrimSuffix(string(line), "\n")
}

// sentCtx builds a cli.Context over a temp Home whose sent.jsonl holds
// lines verbatim, and returns it with its captured stdout and stderr.
func sentCtx(t *testing.T, lines ...string) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	if len(lines) > 0 {
		body := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(sentlog.Path(home), []byte(body), 0o644); err != nil {
			t.Fatalf("write sent log: %v", err)
		}
	}
	var stdout, stderr bytes.Buffer
	return &cli.Context{Home: home, Stdout: &stdout, Stderr: &stderr}, &stdout, &stderr
}

// sentRunJSON runs c against ctx in --json mode and decodes the records.
func sentRunJSON(t *testing.T, c *sentKong, ctx *cli.Context, stdout *bytes.Buffer) []sentlog.Record {
	t.Helper()
	c.JSON = true
	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got []sentlog.Record
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode output %q: %v", stdout.String(), err)
	}
	return got
}

func sentTitles(recs []sentlog.Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Title
	}
	return out
}

// sentFixture is the three-record log the filter tests share: each record
// differs from the others in sender, initiative, and age at once, so a
// filter that silently matches everything fails every test below.
func sentFixture(t *testing.T) []string {
	t.Helper()
	return []string{
		sentLine(t, sentlog.Record{Timestamp: sentTS(3 * time.Hour), Sender: sentlog.KindNotify, Initiative: "at-aaa", Title: "old-notify", Outcome: sentlog.OutcomeSent}),
		sentLine(t, sentlog.Record{Timestamp: sentTS(2 * time.Minute), Sender: sentlog.KindClose, Initiative: "at-bbb", Title: "recent-close", Outcome: sentlog.OutcomeSent}),
		sentLine(t, sentlog.Record{Timestamp: sentTS(1 * time.Minute), Sender: sentlog.KindNotify, Initiative: "at-bbb", Title: "recent-notify", Outcome: sentlog.OutcomeFailed}),
	}
}

func TestSentFilterBySender(t *testing.T) {
	ctx, stdout, _ := sentCtx(t, sentFixture(t)...)
	got := sentRunJSON(t, &sentKong{Sender: string(sentlog.KindClose)}, ctx, stdout)
	if len(got) != 1 || got[0].Title != "recent-close" {
		t.Fatalf("--sender close returned %v, want [recent-close]", sentTitles(got))
	}
}

func TestSentFilterByInitiative(t *testing.T) {
	ctx, stdout, _ := sentCtx(t, sentFixture(t)...)
	got := sentRunJSON(t, &sentKong{Initiative: "at-aaa"}, ctx, stdout)
	if len(got) != 1 || got[0].Title != "old-notify" {
		t.Fatalf("--initiative at-aaa returned %v, want [old-notify]", sentTitles(got))
	}
}

func TestSentFilterBySince(t *testing.T) {
	ctx, stdout, _ := sentCtx(t, sentFixture(t)...)
	got := sentRunJSON(t, &sentKong{Since: 30 * time.Minute}, ctx, stdout)
	// Most recent first, and the 3h-old record is excluded.
	if want := []string{"recent-notify", "recent-close"}; !equalStrings(sentTitles(got), want) {
		t.Fatalf("--since 30m returned %v, want %v", sentTitles(got), want)
	}
}

// TestSentFiltersCombineWithAND pins that the filters intersect rather than
// union: each filter alone matches two of the three fixture records, and
// only one record satisfies all three.
func TestSentFiltersCombineWithAND(t *testing.T) {
	ctx, stdout, _ := sentCtx(t, sentFixture(t)...)
	got := sentRunJSON(t, &sentKong{
		Sender:     string(sentlog.KindNotify),
		Initiative: "at-bbb",
		Since:      30 * time.Minute,
	}, ctx, stdout)
	if len(got) != 1 || got[0].Title != "recent-notify" {
		t.Fatalf("AND'd filters returned %v, want [recent-notify]", sentTitles(got))
	}
}

// TestSentSenderUndeclaredIsQueryableNotSettable pins contract §7
// AMENDMENT 2's asymmetry: seven values are queryable, six are settable.
// UNDECLARED is the row a bug report is most likely to be about, so it must
// be reachable from the CLI — while staying out of knownKinds so no call
// site can ever declare it.
func TestSentSenderUndeclaredIsQueryableNotSettable(t *testing.T) {
	if sentlog.KindUndeclared.Known() {
		t.Fatal("KindUndeclared must not be Known() — no call site may declare it")
	}
	if !queryableSender(string(sentlog.KindUndeclared)) {
		t.Fatal("--sender UNDECLARED must be an accepted filter value (contract §7 amendment 2)")
	}

	lines := append(sentFixture(t), sentLine(t, sentlog.Record{
		Timestamp: sentTS(time.Minute), Sender: sentlog.KindUndeclared, Initiative: "at-ccc", Title: "orphan", Outcome: sentlog.OutcomeSent,
	}))
	ctx, stdout, _ := sentCtx(t, lines...)
	got := sentRunJSON(t, &sentKong{Sender: string(sentlog.KindUndeclared)}, ctx, stdout)
	if len(got) != 1 || got[0].Title != "orphan" {
		t.Fatalf("--sender UNDECLARED returned %v, want [orphan]", sentTitles(got))
	}
}

func TestSentInvalidSenderIsUsageError(t *testing.T) {
	ctx, _, _ := sentCtx(t, sentFixture(t)...)
	err := (&sentKong{Sender: "bogus"}).Run(ctx)
	if err == nil {
		t.Fatal("--sender bogus should be rejected")
	}
	if !strings.Contains(err.Error(), "notify") || !strings.Contains(err.Error(), string(sentlog.KindUndeclared)) {
		t.Fatalf("usage error should list every queryable kind, got %q", err)
	}
}

// TestSentTruncatesBodyByDefaultNotWithFull covers both halves of §7's
// truncation rule, including the newline collapse the tabwriter table needs
// to keep its columns aligned.
func TestSentTruncatesBodyByDefaultNotWithFull(t *testing.T) {
	// FIXTURE DESIGN, both halves load-bearing — a repeated-character body
	// cannot witness this and silently weakens every assertion below.
	//
	// Non-repeating content: sentBodySeq gives every rune index a distinct
	// neighbourhood, so the exact cut POSITION is observable in the output.
	// With a homogeneous run like "aaaa…" a cut anywhere in a wide range
	// produces output a substring check still accepts.
	//
	// Whitespace INSIDE the budget (at rune 50, well under 200): this is
	// what makes collapse-then-truncate and truncate-then-collapse diverge.
	// Past the cutoff the two orderings are byte-identical and a swapped
	// implementation passes.
	// Contract §7 fixes the budget at 200 runes. Restated here as a LITERAL,
	// never derived from sentBodyTruncRunes: an expectation computed from
	// the constant under test follows that constant wherever it goes and so
	// can never witness it being wrong. This guard is what catches the
	// budget itself drifting.
	const wantTruncRunes = 200
	if sentBodyTruncRunes != wantTruncRunes {
		t.Fatalf("contract §7 fixes the truncated body at %d runes, but sentBodyTruncRunes is %d", wantTruncRunes, sentBodyTruncRunes)
	}

	const headRunes = 50
	head := sentBodySeq(0, headRunes)
	tail := sentBodySeq(headRunes, 300)
	body := head + "\n\t  \n" + tail

	// Collapse first: the five-character whitespace run becomes ONE space,
	// so a full 199 runes of content survive the cut. Truncate first and
	// five of those runes are spent on whitespace about to be discarded,
	// leaving four fewer content runes.
	wantCell := head + " " + sentBodySeq(headRunes, wantTruncRunes-1-headRunes-1) + "…"
	if got := len([]rune(wantCell)); got != wantTruncRunes {
		t.Fatalf("fixture is wrong: expected cell is %d runes, want %d", got, wantTruncRunes)
	}

	lines := []string{sentLine(t, sentlog.Record{
		Timestamp: sentTS(time.Minute), Sender: sentlog.KindNotify, Initiative: "at-aaa", Title: "big", Body: body, Outcome: sentlog.OutcomeSent,
	})}

	ctx, stdout, _ := sentCtx(t, lines...)
	if err := (&sentKong{}).Run(ctx); err != nil {
		t.Fatalf("default Run: %v", err)
	}
	table := stdout.String()
	if !strings.Contains(table, "TIME") || !strings.Contains(table, "BODY") {
		t.Fatalf("default output is not the §7 table:\n%s", table)
	}

	// Header, separator, one row — a multi-line body must not spill into
	// extra rows and break the column alignment.
	rows := strings.Split(strings.TrimRight(table, "\n"), "\n")
	if len(rows) != 3 {
		t.Fatalf("a multi-line body must collapse to one table row, got %d lines:\n%s", len(rows), table)
	}

	// EXACT EQUALITY on the isolated cell, never strings.Contains: a
	// containment check on self-similar content slides, and would accept a
	// whole range of wrong cut points. tabwriter pads with two or more
	// spaces and the four preceding columns are space-free, so splitting on
	// runs of 2+ spaces isolates the BODY cell exactly — the single space
	// inside the collapsed body does not split.
	cells := regexp.MustCompile(` {2,}`).Split(rows[2], -1)
	if len(cells) != 5 {
		t.Fatalf("expected 5 table cells, got %d: %q", len(cells), rows[2])
	}
	if cells[4] != wantCell {
		t.Fatalf("BODY cell wrong — whitespace must be collapsed BEFORE truncating, and the cut must land at %d runes.\n got: %q\nwant: %q", wantTruncRunes, cells[4], wantCell)
	}

	ctxFull, stdoutFull, _ := sentCtx(t, lines...)
	if err := (&sentKong{Full: true}).Run(ctxFull); err != nil {
		t.Fatalf("--full Run: %v", err)
	}
	if !strings.Contains(stdoutFull.String(), body) {
		t.Fatalf("--full must print the whole body, got:\n%s", stdoutFull.String())
	}

	ctxJSON, stdoutJSON, _ := sentCtx(t, lines...)
	got := sentRunJSON(t, &sentKong{}, ctxJSON, stdoutJSON)
	if len(got) != 1 || got[0].Body != body {
		t.Fatalf("--json must never truncate the body, got %q", got[0].Body)
	}
}

// TestSentSkipsUnparseableTimestamp covers the skip branch for a line that
// IS valid JSON but whose ts fails time.Parse(RFC3339) — the twin of the
// invalid-JSON case in tests/sent-log.test.sh case6. A single bad ts must
// never make the rest of the audit trail unreadable.
func TestSentSkipsUnparseableTimestamp(t *testing.T) {
	bad := sentLine(t, sentlog.Record{Timestamp: "24/07/2026 6pm", Sender: sentlog.KindNotify, Initiative: "at-aaa", Title: "bad-ts", Outcome: sentlog.OutcomeSent})
	if !json.Valid([]byte(bad)) {
		t.Fatalf("fixture must be syntactically valid JSON: %s", bad)
	}
	good := sentLine(t, sentlog.Record{Timestamp: sentTS(time.Minute), Sender: sentlog.KindNotify, Initiative: "at-aaa", Title: "good-ts", Outcome: sentlog.OutcomeSent})

	ctx, stdout, stderr := sentCtx(t, bad, good)
	got := sentRunJSON(t, &sentKong{}, ctx, stdout)
	if len(got) != 1 || got[0].Title != "good-ts" {
		t.Fatalf("record with an unparseable ts should be skipped, got %v", sentTitles(got))
	}
	if !strings.Contains(stderr.String(), "unparseable timestamp") {
		t.Fatalf("skipping a bad ts should warn on stderr, got %q", stderr.String())
	}
}

// TestSentNonPositiveLimitUsesDefault pins that --limit 0 and a negative
// --limit mean the documented default of 20, not "unlimited".
func TestSentNonPositiveLimitUsesDefault(t *testing.T) {
	var lines []string
	for i := 0; i < sentDefaultLimit+5; i++ {
		lines = append(lines, sentLine(t, sentlog.Record{
			Timestamp: sentTS(time.Duration(i) * time.Minute), Sender: sentlog.KindNotify, Initiative: "at-aaa", Title: fmt.Sprintf("r%02d", i), Outcome: sentlog.OutcomeSent,
		}))
	}

	for _, limit := range []int{0, -7} {
		ctx, stdout, _ := sentCtx(t, lines...)
		got := sentRunJSON(t, &sentKong{Limit: limit}, ctx, stdout)
		if len(got) != sentDefaultLimit {
			t.Fatalf("--limit %d returned %d records, want the default %d", limit, len(got), sentDefaultLimit)
		}
		// Most recent first: the newest record is the 0-minutes-old one.
		if got[0].Title != "r00" {
			t.Fatalf("--limit %d: first record %q, want the most recent r00", limit, got[0].Title)
		}
	}

	ctx, stdout, _ := sentCtx(t, lines...)
	if got := sentRunJSON(t, &sentKong{Limit: 3}, ctx, stdout); len(got) != 3 {
		t.Fatalf("--limit 3 returned %d records, want 3", len(got))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
