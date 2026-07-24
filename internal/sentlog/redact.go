package sentlog

import (
	"net/url"
	"regexp"
)

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
	return urlPattern.ReplaceAllStringFunc(err.Error(), redactURL)
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
