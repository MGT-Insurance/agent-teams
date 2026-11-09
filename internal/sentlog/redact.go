package sentlog

import (
	"net/url"
	"regexp"
)

// secretPattern matches text shaped like a credential or chat id: a run of 5
// or more digits, or a run of 24 or more letters, digits, underscores, and
// hyphens (bot tokens, API keys, ids).
var secretPattern = regexp.MustCompile(`\d{5,}|[A-Za-z0-9_-]{24,}`)

// urlPattern matches a URL substring starting at a scheme (e.g. "https://")
// and running until whitespace or a character that commonly closes a quoted
// URL inside a Go error string (a quote, backtick, or closing bracket/paren
// — e.g. `Get "https://host/path": dial tcp ...`).
var urlPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s"'` + "`" + `)>\]]+`)

// RedactError renders err's message with every embedded URL reduced to
// "scheme://host" — userinfo, path, and query stripped — so a credential
// embedded in a URL (a bot token in the path, a password in userinfo, an API
// key in the query string) never reaches the sent-message log.
//
// After the URL pass, every run of 5 or more digits and every token-shaped
// string of 24 or more characters becomes "<redacted>", so a credential or
// chat id that sits outside a URL does not reach the log either.
//
// This is a GENERIC backstop, not a replacement for any transport's own
// sanitizer. loggingTransport (internal/transport) wraps whatever Transport
// For returns and cannot call into that transport's unexported
// error-cleaning logic (today, Telegram's sanitizeTransportErr) — so it must
// not assume any transport-specific guarantee holds. This is the one,
// transport-agnostic rule it applies to whatever error string it is handed
// (contract agent-teams-48dh.1 §6, amended).
//
// Returns "" for a nil err.
func RedactError(err error) string {
	if err == nil {
		return ""
	}
	msg := urlPattern.ReplaceAllStringFunc(err.Error(), redactURL)
	return secretPattern.ReplaceAllString(msg, "<redacted>")
}

// redactURL parses one matched URL substring and rebuilds it as
// "scheme://host" only. A substring that fails to parse as a URL (should not
// happen given urlPattern's shape, but error text can contain anything) is
// replaced with a fixed placeholder rather than passed through unredacted —
// when in doubt, redact.
func redactURL(match string) string {
	u, err := url.Parse(match)
	if err != nil || u.Host == "" {
		return "<redacted-url>"
	}
	return u.Scheme + "://" + u.Host
}
