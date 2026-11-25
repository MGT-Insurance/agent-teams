package verbs

import (
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/po"
)

// PO state keys (po:<area>:<kind>...) share the "po:" prefix with the shared
// "po" learnings role (po:hot:<slug>, po:fresh:<slug>, po:<slug>). Every verb
// that reads a role's keys by prefix, or splits keys on the first colon to
// find roles, would otherwise treat a PO's claims, view, said and readings as
// role learnings: served at startup, matched by recall, listed in memories-json,
// and swept up (and forgotten) by condense. isPOStateKey recognizes the state
// keys by the contract key scheme so those verbs can skip them. Without any
// po:<area>:... key the filter changes nothing.

// isPOStateKey reports whether k is a PO state key: "po:" then an area, then a
// suffix that po.KindOf recognizes (view, claim:..., said:..., reading:...).
func isPOStateKey(k string) bool {
	rest, ok := strings.CutPrefix(k, po.RoleShared+":")
	if !ok {
		return false
	}
	area, suffix, ok := strings.Cut(rest, ":")
	// A tier word in the area slot (po:hot:view) is a real learning, not state.
	return ok && !po.IsReservedArea(area) && po.KindOf(suffix) != ""
}

// dropPOStateKeys removes every PO state key from raw.
func dropPOStateKeys[V any](raw map[string]V) {
	for k := range raw {
		if isPOStateKey(k) {
			delete(raw, k)
		}
	}
}
