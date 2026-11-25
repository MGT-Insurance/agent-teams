package initiative

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// GateClasses is the CLOSED gate-class set used by steward-approves and the
// autonomy "gates=" list (docs/outsider-requests.md). Never extended without a
// contract bead.
var GateClasses = []string{"plan", "scope", "design-fork", "live-test", "delivery"}

// OriginOutsider is the only accepted value of the "origin" field.
const OriginOutsider = "outsider"

// MaxAutonomyWindow is the longest an active autonomy grant may run: "until"
// is at most this far after the grant.
const MaxAutonomyWindow = 72 * time.Hour

var requesterPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*/[a-z0-9]+(-[a-z0-9]+)*$`)

// splitClasses parses a comma list; "" gives nil. It does not validate.
func splitClasses(v string) []string {
	if v == "" {
		return nil
	}
	var out []string
	for _, c := range strings.Split(v, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func validClassList(list []string) error {
	for _, c := range list {
		if !slices.Contains(GateClasses, c) {
			return fmt.Errorf("unknown gate class %q (closed set: %s)", c, strings.Join(GateClasses, ", "))
		}
	}
	return nil
}

// validateOutsiderFields is New's write-side check of the outsider fields.
// Of stays lenient (it never rejects stored data).
func validateOutsiderFields(f Fields) error {
	if f.Origin != "" && f.Origin != OriginOutsider {
		return fmt.Errorf("initiative.New: origin must be %q, got %q", OriginOutsider, f.Origin)
	}
	if f.Requester != "" && !requesterPattern.MatchString(f.Requester) {
		return fmt.Errorf("initiative.New: requester must be <owner-handle>/<agent-name> (lowercase kebab tokens, one slash), got %q", f.Requester)
	}
	if err := validClassList(f.StewardApproves); err != nil {
		return fmt.Errorf("initiative.New: steward-approves: %w", err)
	}
	for _, c := range f.StewardApproves {
		if strings.Contains(c, ",") {
			return fmt.Errorf("initiative.New: steward-approves class must not contain a comma: %q", c)
		}
	}
	if f.Autonomy != "" {
		if _, err := ParseAutonomy(f.Autonomy); err != nil {
			return fmt.Errorf("initiative.New: autonomy: %w", err)
		}
	}
	return nil
}

// Autonomy is a parsed autonomy line.
type Autonomy struct {
	Revoked bool
	Gates   []string  // active only
	Until   time.Time // active only
	At      time.Time // revoked only: when it was revoked
	Ref     string
}

// ParseAutonomy parses the two frozen autonomy grammars:
//
//	active:  gates=<class list>; until=<RFC3339 UTC>; by=eric; via=steward; ref=<source ref>
//	revoked: revoked <RFC3339 UTC>; ref=<source ref>
//
// The class list is comma-separated over GateClasses. Timestamps are RFC3339
// UTC ("Z" form). ref is the remainder of the line and must be non-empty.
func ParseAutonomy(v string) (Autonomy, error) {
	if rest, ok := strings.CutPrefix(v, "revoked "); ok {
		parts := strings.SplitN(rest, "; ", 2)
		if len(parts) != 2 {
			return Autonomy{}, fmt.Errorf("revoked autonomy needs \"revoked <time>; ref=<ref>\"")
		}
		at, err := parseUTC(parts[0])
		if err != nil {
			return Autonomy{}, err
		}
		ref, ok := strings.CutPrefix(parts[1], "ref=")
		if !ok || ref == "" {
			return Autonomy{}, fmt.Errorf("revoked autonomy needs a non-empty ref=")
		}
		return Autonomy{Revoked: true, At: at, Ref: ref}, nil
	}
	parts := strings.SplitN(v, "; ", 5)
	if len(parts) != 5 {
		return Autonomy{}, fmt.Errorf("active autonomy needs gates=; until=; by=; via=; ref=")
	}
	gatesV, ok1 := strings.CutPrefix(parts[0], "gates=")
	untilV, ok2 := strings.CutPrefix(parts[1], "until=")
	by, ok3 := strings.CutPrefix(parts[2], "by=")
	via, ok4 := strings.CutPrefix(parts[3], "via=")
	ref, ok5 := strings.CutPrefix(parts[4], "ref=")
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return Autonomy{}, fmt.Errorf("active autonomy fields must be gates=, until=, by=, via=, ref= in that order")
	}
	gates := splitClasses(gatesV)
	if len(gates) == 0 {
		return Autonomy{}, fmt.Errorf("active autonomy needs at least one gate class")
	}
	if err := validClassList(gates); err != nil {
		return Autonomy{}, err
	}
	until, err := parseUTC(untilV)
	if err != nil {
		return Autonomy{}, err
	}
	if by != "eric" || via != "steward" {
		return Autonomy{}, fmt.Errorf("active autonomy must be by=eric; via=steward")
	}
	if ref == "" {
		return Autonomy{}, fmt.Errorf("active autonomy needs a non-empty ref=")
	}
	return Autonomy{Gates: gates, Until: until, Ref: ref}, nil
}

func parseUTC(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil || !strings.HasSuffix(s, "Z") {
		return time.Time{}, fmt.Errorf("%q is not an RFC3339 UTC time (…Z)", s)
	}
	return t, nil
}

// ActiveAutonomy reports the gate classes and source ref of f's autonomy line
// when it is an ACTIVE grant at now. Revoked, expired (until not after now),
// malformed, or until more than MaxAutonomyWindow after now all give ok=false.
// (The line carries no grant time, so the 72h cap is checked against now: a
// legitimate grant always has until <= grant+72h <= now+72h.)
func ActiveAutonomy(f Fields, now time.Time) (gates []string, ref string, ok bool) {
	if f.Autonomy == "" {
		return nil, "", false
	}
	a, err := ParseAutonomy(f.Autonomy)
	if err != nil || a.Revoked {
		return nil, "", false
	}
	if !a.Until.After(now) || a.Until.After(now.Add(MaxAutonomyWindow)) {
		return nil, "", false
	}
	return a.Gates, a.Ref, true
}
