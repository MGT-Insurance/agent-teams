package transport

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Logf writes one timestamped, indentation-scoped log line to w. depth 0 is
// a poll-cycle-level line; depth 1+ nests an event under it via a two-space
// indent per level, e.g.:
//
//	2026-07-21 14:32:10 poll: offset=118 -> 121, 2 update(s)
//	2026-07-21 14:32:10   received message (thread="42"): "looks good, ship it"
//	2026-07-21 14:32:10     routed to initiative at-001 (Blocked on review)
//
// Local time (time.Now(), no UTC flag) per Eric's preference.
func Logf(w io.Writer, depth int, format string, args ...any) {
	fmt.Fprintf(w, "%s %s%s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		strings.Repeat("  ", depth),
		fmt.Sprintf(format, args...))
}

// PreviewText returns the first n runes of s (rune-safe, mirrors
// truncateUTF8's concern in telegram.go), with "..." appended if truncated.
func PreviewText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
