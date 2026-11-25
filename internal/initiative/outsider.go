package initiative

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// GateClasses is the CLOSED gate-class set used by steward-approves
// (docs/outsider-requests.md). Never extended without a contract bead.
var GateClasses = []string{"plan", "scope", "design-fork", "live-test", "delivery"}

// OriginOutsider is the only accepted value of the "origin" field.
const OriginOutsider = "outsider"

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
	if strings.ContainsAny(f.Linear, " \t\r\n") {
		return fmt.Errorf("initiative.New: linear must be a single issue key with no whitespace, got %q", f.Linear)
	}
	if err := validClassList(f.StewardApproves); err != nil {
		return fmt.Errorf("initiative.New: steward-approves: %w", err)
	}
	for _, c := range f.StewardApproves {
		if strings.Contains(c, ",") {
			return fmt.Errorf("initiative.New: steward-approves class must not contain a comma: %q", c)
		}
	}
	return nil
}
