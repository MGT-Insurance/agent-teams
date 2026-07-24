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
	"unicode/utf8"

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
	// --full prefixes every body line, so the body is recovered from the
	// block rather than matched verbatim. Exact equality on the recovered
	// body is the untruncated guarantee.
	blocks := sentParseFull(t, stdoutFull.String())
	if len(blocks) != 1 {
		t.Fatalf("--full over one record produced %d blocks:\n%s", len(blocks), stdoutFull.String())
	}
	if blocks[0].body != body {
		t.Fatalf("--full must print the whole body.\n got: %q\nwant: %q", blocks[0].body, body)
	}

	ctxJSON, stdoutJSON, _ := sentCtx(t, lines...)
	got := sentRunJSON(t, &sentKong{}, ctxJSON, stdoutJSON)
	if len(got) != 1 || got[0].Body != body {
		t.Fatalf("--json must never truncate the body, got %q", got[0].Body)
	}
}

// sentCJKSeq returns n DISTINCT runes from the CJK Unified Ideographs
// block, starting at offset start. Two properties, both load-bearing:
// every rune is 3 bytes in UTF-8, which is what makes a byte-based cut
// observable at all; and they are distinct rather than a repeated
// "漢字漢字…", so the cut POSITION stays observable here too and the exact
// -equality assertion below cannot slide.
func sentCJKSeq(start, n int) string {
	var b strings.Builder
	for i := start; i < start+n; i++ {
		b.WriteRune(rune(0x4e00 + i))
	}
	return b.String()
}

// TestSentTruncatesByRunesNotBytes pins the budget's UNIT. It is a separate
// test from the ASCII case rather than an extra record in that fixture
// because the two pin different properties and neither substitutes for the
// other: the ASCII body carries whitespace inside the budget, which is what
// witnesses collapse-before-truncate, and a non-ASCII body is the only
// thing that can witness runes-not-bytes.
//
// This is the regression contract §7 AMENDMENT 3 was raised to prevent. §7
// as frozen named telegram.truncateUTF8, which is BYTE-based; amendment 3
// superseded it with verbs.truncate precisely because 200 bytes of CJK is
// about 66 runes. An all-ASCII fixture cannot tell the two apart — byte
// count equals rune count — so without this case the suite pinned every
// part of amendment 3 except its reason for existing. Measured with a
// byte-truncating binary on a 300-character CJK body: 68 runes shown
// instead of 200, and the cut landed mid-codepoint, writing U+FFFD into a
// durable audit record.
func TestSentTruncatesByRunesNotBytes(t *testing.T) {
	// Contract §7 fixes the budget at 200 RUNES. Restated as a literal,
	// never derived from sentBodyTruncRunes, for the same reason as in the
	// ASCII case: an expectation computed from the constant under test
	// follows that constant wherever it goes.
	const wantTruncRunes = 200

	const bodyRunes = 300
	body := sentCJKSeq(0, bodyRunes)

	// The fixture guard. If someone ever swaps this body for an ASCII one
	// the test still passes against a byte-based cut, and silently stops
	// testing anything — which is exactly how this hole was left open the
	// first time.
	if len(body) == len([]rune(body)) {
		t.Fatalf("fixture must be multi-byte or it cannot witness a byte-based cut: %d bytes, %d runes", len(body), len([]rune(body)))
	}

	// No whitespace in the body, so the collapse is a no-op here and the
	// cut is the only thing under test: 199 runes of content plus the
	// ellipsis. A byte-based cut at 199 BYTES yields 66 runes and a split
	// codepoint.
	wantCell := sentCJKSeq(0, wantTruncRunes-1) + "…"
	if got := len([]rune(wantCell)); got != wantTruncRunes {
		t.Fatalf("fixture is wrong: expected cell is %d runes, want %d", got, wantTruncRunes)
	}

	lines := []string{sentLine(t, sentlog.Record{
		Timestamp: sentTS(time.Minute), Sender: sentlog.KindNotify, Initiative: "at-cjk", Title: "cjk", Body: body, Outcome: sentlog.OutcomeSent,
	})}

	ctx, stdout, _ := sentCtx(t, lines...)
	if err := (&sentKong{}).Run(ctx); err != nil {
		t.Fatalf("default Run: %v", err)
	}
	table := stdout.String()

	rows := strings.Split(strings.TrimRight(table, "\n"), "\n")
	if len(rows) != 3 {
		t.Fatalf("expected header, separator and one row, got %d lines:\n%s", len(rows), table)
	}
	// Same isolation as the ASCII case: the four preceding columns are
	// space-free, tabwriter pads with 2+ spaces, and this body contains no
	// spaces at all.
	cells := regexp.MustCompile(` {2,}`).Split(rows[2], -1)
	if len(cells) != 5 {
		t.Fatalf("expected 5 table cells, got %d: %q", len(cells), rows[2])
	}
	cell := cells[4]

	// Three independent properties of the same cell, reported with Errorf
	// rather than Fatalf so a byte-based cut trips all three visibly. Under
	// Fatalf the first failure would hide the other two, leaving them
	// unproven against the mutation they exist to catch — a dead assertion
	// nobody would notice.
	if !utf8.ValidString(cell) {
		t.Errorf("BODY cell is not valid UTF-8 — the cut split a codepoint: %q", cell)
	}
	if got := len([]rune(cell)); got != wantTruncRunes {
		t.Errorf("BODY cell is %d runes, want %d — the budget is RUNES, not bytes (contract §7 amendment 3)", got, wantTruncRunes)
	}
	if cell != wantCell {
		t.Errorf("BODY cell wrong.\n got: %q\nwant: %q", cell, wantCell)
	}

	// --json still carries the whole body, multi-byte and untruncated.
	ctxJSON, stdoutJSON, _ := sentCtx(t, lines...)
	got := sentRunJSON(t, &sentKong{}, ctxJSON, stdoutJSON)
	if len(got) != 1 || got[0].Body != body {
		t.Fatalf("--json must never truncate the body, got %d runes", len([]rune(got[0].Body)))
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

// ── agent-teams-48dh.11: ordering is total, most-recent-first ────────────

// sentSameSecondFixture returns n log lines in file (append) order, titled
// r00..r{n-1}, where records 2k and 2k+1 share a timestamp SECOND. That is
// the on-disk reality, not a contrivance: the log stores RFC3339 at
// one-second granularity, so any two sends inside the same second tie.
//
// Titles are distinct and non-repeating, so every position in the output is
// individually identifiable and a mis-ordering cannot hide behind a
// self-similar fixture.
func sentSameSecondFixture(t *testing.T, n int) []string {
	t.Helper()
	base := time.Date(2026, 7, 24, 17, 37, 25, 0, time.UTC)
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = sentLine(t, sentlog.Record{
			Timestamp:  base.Add(time.Duration(i/2) * time.Second).Format(time.RFC3339),
			Sender:     sentlog.KindNotify,
			Initiative: "at-tie",
			Title:      fmt.Sprintf("r%02d", i),
			Outcome:    sentlog.OutcomeSent,
		})
	}
	return lines
}

// TestSentOrdersSameSecondRecordsByReverseFileOrder pins that ties on the
// one-second timestamp are broken by REVERSE file order, making
// "most recent first" total.
//
// Both sizes are load-bearing and neither is redundant: Go's sort switches
// from insertion sort to pdqsort above 12 elements, and the two failure
// modes differ. At n<=12 an unstable-but-insertion sort silently yields
// OLDEST-first inside each second; above it, ties come out arbitrarily.
//
// The expected orders are written out as literals rather than computed by
// reversing the fixture, so nothing about the expectation can move with the
// code — or with the fixture builder — under test.
func TestSentOrdersSameSecondRecordsByReverseFileOrder(t *testing.T) {
	cases := []struct {
		n    int
		want []string
	}{
		{12, []string{"r11", "r10", "r09", "r08", "r07", "r06", "r05", "r04", "r03", "r02", "r01", "r00"}},
		{30, []string{
			"r29", "r28", "r27", "r26", "r25", "r24", "r23", "r22", "r21", "r20",
			"r19", "r18", "r17", "r16", "r15", "r14", "r13", "r12", "r11", "r10",
			"r09", "r08", "r07", "r06", "r05", "r04", "r03", "r02", "r01", "r00",
		}},
	}
	// Subtests, not a bare loop: a t.Fatalf on the first size would stop the
	// second from ever running, and the two sizes exercise different sort
	// algorithms — leaving one silently unexercised is how a break-probe
	// gets a false reading.
	for _, tc := range cases {
		t.Run(fmt.Sprintf("n=%d", tc.n), func(t *testing.T) {
			ctx, stdout, _ := sentCtx(t, sentSameSecondFixture(t, tc.n)...)
			got := sentRunJSON(t, &sentKong{Limit: 100}, ctx, stdout)
			if !equalStrings(sentTitles(got), tc.want) {
				t.Fatalf("order must be the exact reverse of file order (ties broken by reverse file order).\n got: %v\nwant: %v",
					sentTitles(got), tc.want)
			}
		})
	}
}

// TestSentLimitKeepsTheNewestSameSecondRecords is the measured data loss:
// against a 30-record log whose records tie in pairs, --limit 3 returned
// r29, r28, r26 — dropping r27, which is present in the log, and showing an
// OLDER record in its place. A false negative in the one query this log
// exists to answer.
func TestSentLimitKeepsTheNewestSameSecondRecords(t *testing.T) {
	ctx, stdout, _ := sentCtx(t, sentSameSecondFixture(t, 30)...)
	got := sentRunJSON(t, &sentKong{Limit: 3}, ctx, stdout)
	want := []string{"r29", "r28", "r27"}
	if !equalStrings(sentTitles(got), want) {
		t.Fatalf("--limit 3 must return the last 3 records appended.\n got: %v\nwant: %v", sentTitles(got), want)
	}
}

// ── agent-teams-48dh.12: human-readable modes cannot be forged ───────────

// sentFullBlock is one parsed --full record block.
type sentFullBlock struct {
	header, title, body string
}

// sentParseFull parses --full output back into records using ONLY the
// documented block structure: a header at column 0, "  title: ", "  body:",
// then every body line under sentFullBodyPrefix, terminated by an empty
// line. It is deliberately strict — an unparseable line is a failure, not a
// skip — because "body text produced a line that isn't a legal body line"
// is exactly the bug under test.
func sentParseFull(t *testing.T, out string) []sentFullBlock {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	var blocks []sentFullBlock
	for i := 0; i < len(lines); {
		var b sentFullBlock
		b.header = lines[i]
		if strings.HasPrefix(b.header, " ") || b.header == "" {
			t.Fatalf("line %d is not a record header at column 0: %q\nfull output:\n%s", i, b.header, out)
		}
		i++
		if i >= len(lines) || !strings.HasPrefix(lines[i], "  title: ") {
			t.Fatalf("line %d should be the title line, got %q\nfull output:\n%s", i, lines[i], out)
		}
		b.title = strings.TrimPrefix(lines[i], "  title: ")
		i++
		if i >= len(lines) || lines[i] != "  body:" {
			t.Fatalf("line %d should be %q, got %q\nfull output:\n%s", i, "  body:", lines[i], out)
		}
		i++
		var body []string
		for i < len(lines) && strings.HasPrefix(lines[i], sentFullBodyPrefix) {
			body = append(body, strings.TrimPrefix(lines[i], sentFullBodyPrefix))
			i++
		}
		b.body = strings.Join(body, "\n")
		if i < len(lines) {
			if lines[i] != "" {
				t.Fatalf("line %d should be the empty record separator, got %q\nfull output:\n%s", i, lines[i], out)
			}
			i++
		}
		blocks = append(blocks, b)
	}
	return blocks
}

// sentForgedBody is a complete, well-formed fake record block. Rendered raw
// by --full it produced a THIRD block from a log holding TWO records, using
// nothing but newlines — no escape codes needed.
const sentForgedBody = "innocuous first line\n" +
	"2026-07-24T00:00:00Z  close  initiative=at-other  outcome=failed\n" +
	"  title: FABRICATED RECORD\n" +
	"  body: this record does not exist in sent.jsonl"

// TestSentFullCannotForgeARecordFromBodyText pins that a body containing a
// verbatim record block still renders as ONE record's body.
func TestSentFullCannotForgeARecordFromBodyText(t *testing.T) {
	lines := []string{
		sentLine(t, sentlog.Record{Timestamp: "2026-07-24T17:38:50Z", Sender: sentlog.KindNotify, Initiative: "at-real", Title: "real1", Body: "genuinely sent by notify", Outcome: sentlog.OutcomeSent}),
		sentLine(t, sentlog.Record{Timestamp: "2026-07-24T17:40:30Z", Sender: sentlog.KindNotify, Initiative: "at-evil", Title: "spoof", Body: sentForgedBody, Outcome: sentlog.OutcomeSent}),
	}

	ctx, stdout, _ := sentCtx(t, lines...)
	if err := (&sentKong{Full: true}).Run(ctx); err != nil {
		t.Fatalf("--full Run: %v", err)
	}
	out := stdout.String()

	// The log holds two records; the display must hold two records. Counted
	// on the header shape, which is what a reader scans for.
	if n := len(regexp.MustCompile(`(?m)^[^ \n].*  initiative=`).FindAllString(out, -1)); n != 2 {
		t.Fatalf("2 records on disk rendered as %d record headers — body text forged a record:\n%s", n, out)
	}

	blocks := sentParseFull(t, out)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d:\n%s", len(blocks), out)
	}
	if blocks[0].title != "spoof" || blocks[1].title != "real1" {
		t.Fatalf("blocks out of order: %q, %q", blocks[0].title, blocks[1].title)
	}
	// Unforgeable AND untruncated: the whole forged body is still shown, it
	// just cannot escape its record.
	if blocks[0].body != sentForgedBody {
		t.Fatalf("body must be shown whole inside its own record.\n got: %q\nwant: %q", blocks[0].body, sentForgedBody)
	}

	// --json is unaffected and stays byte-exact.
	ctxJSON, stdoutJSON, _ := sentCtx(t, lines...)
	got := sentRunJSON(t, &sentKong{}, ctxJSON, stdoutJSON)
	if len(got) != 2 || got[0].Body != sentForgedBody {
		t.Fatalf("--json must return the body byte-exact, got %q", got[0].Body)
	}
}

// sentControlBody carries one of each hostile class: NUL, BEL, BS, a real
// CSI sequence, and an unterminated RIGHT-TO-LEFT OVERRIDE. Every rune is
// distinct so the position of each substitution is observable.
const sentControlBody = "a\x00b\x07c\x08d\x1b[2Ke\u202Ef"

// sentControlBodyRendered is what those runes must render as. Written as a
// LITERAL, never derived from sentSanitize: an expectation computed from the
// function under test follows it wherever it goes.
const sentControlBodyRendered = "a\uFFFDb\uFFFDc\uFFFDd\uFFFD[2Ke\uFFFDf"

// sentTerminalActiveRunes returns every rune in s that a terminal could act
// on structurally — C0 controls, DEL, C1 controls, and the bidi
// embedding/override/isolate formatters — excluding the runes in allowed.
func sentTerminalActiveRunes(s, allowed string) []rune {
	var found []rune
	for _, r := range s {
		if strings.ContainsRune(allowed, r) {
			continue
		}
		switch {
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f,
			r == 0x200e, r == 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			found = append(found, r)
		}
	}
	return found
}

// TestSentHumanModesNeverEmitControlCharacters pins that neither the table
// nor --full lets a body drive the terminal. The table is the one that
// mattered most: records print most-recent-first, so an OLD body sits below
// newer rows and a CUU+EL sequence repaints them — the probe measured the
// newest genuine record vanishing from the screen and a fabricated row in
// its place, with nothing on disk changed.
func TestSentHumanModesNeverEmitControlCharacters(t *testing.T) {
	lines := []string{sentLine(t, sentlog.Record{
		Timestamp: "2026-07-24T17:35:14Z", Sender: sentlog.KindNotify, Initiative: "at-quiet",
		Title: "ctl", Body: sentControlBody, Outcome: sentlog.OutcomeSent,
	})}

	// Default table: nothing survives, not even a tab or newline inside a cell.
	ctx, stdout, _ := sentCtx(t, lines...)
	if err := (&sentKong{}).Run(ctx); err != nil {
		t.Fatalf("default Run: %v", err)
	}
	table := stdout.String()
	if bad := sentTerminalActiveRunes(table, "\n"); len(bad) != 0 {
		t.Fatalf("table output must contain no terminal-active runes, found %U in:\n%q", bad, table)
	}

	// Exact equality on the isolated BODY cell — a containment check would
	// accept a partially-sanitized cell. The four preceding columns are
	// space-free and tabwriter pads with 2+ spaces, so this split isolates
	// the cell exactly; the sanitized body contains no spaces either.
	rows := strings.Split(strings.TrimRight(table, "\n"), "\n")
	if len(rows) != 3 {
		t.Fatalf("expected header, separator and one row, got %d lines:\n%s", len(rows), table)
	}
	cells := regexp.MustCompile(` {2,}`).Split(rows[2], -1)
	if len(cells) != 5 {
		t.Fatalf("expected 5 table cells, got %d: %q", len(cells), rows[2])
	}
	if cells[4] != sentControlBodyRendered {
		t.Fatalf("BODY cell wrong.\n got: %q\nwant: %q", cells[4], sentControlBodyRendered)
	}

	// --full: same treatment. LF and TAB are the only survivors, and both
	// are structurally harmless there because every body line is prefixed.
	ctxFull, stdoutFull, _ := sentCtx(t, lines...)
	if err := (&sentKong{Full: true}).Run(ctxFull); err != nil {
		t.Fatalf("--full Run: %v", err)
	}
	full := stdoutFull.String()
	if bad := sentTerminalActiveRunes(full, "\n\t"); len(bad) != 0 {
		t.Fatalf("--full output must contain no terminal-active runes, found %U in:\n%q", bad, full)
	}
	blocks := sentParseFull(t, full)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d:\n%s", len(blocks), full)
	}
	if blocks[0].body != sentControlBodyRendered {
		t.Fatalf("--full body wrong.\n got: %q\nwant: %q", blocks[0].body, sentControlBodyRendered)
	}

	// --json is the machine path and must still carry the original bytes.
	ctxJSON, stdoutJSON, _ := sentCtx(t, lines...)
	got := sentRunJSON(t, &sentKong{}, ctxJSON, stdoutJSON)
	if len(got) != 1 || got[0].Body != sentControlBody {
		t.Fatalf("--json must return the body byte-exact, got %q", got[0].Body)
	}
}

// TestSentTableSanitizesEveryColumnNotJustTheBody pins that the non-body
// columns are sanitized too. They are copied from the log verbatim, so a
// forged record's initiative field is as reachable as its body — sanitizing
// only the body would leave the forgery property false.
func TestSentTableSanitizesEveryColumnNotJustTheBody(t *testing.T) {
	lines := []string{sentLine(t, sentlog.Record{
		Timestamp: "2026-07-24T17:35:14Z", Sender: sentlog.KindNotify,
		Initiative: "at-\x1b[2Kx", Title: "t", Body: "clean", Outcome: sentlog.OutcomeSent,
	})}
	ctx, stdout, _ := sentCtx(t, lines...)
	if err := (&sentKong{}).Run(ctx); err != nil {
		t.Fatalf("default Run: %v", err)
	}
	if bad := sentTerminalActiveRunes(stdout.String(), "\n"); len(bad) != 0 {
		t.Fatalf("INITIATIVE column must be sanitized too, found %U in:\n%q", bad, stdout.String())
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
