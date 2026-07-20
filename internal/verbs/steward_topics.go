// This file is owned by the synced-topics track (agent-teams-5y8a.3): the
// publish/read halves of the multi-machine steward-topics record whose key
// convention and value schema are frozen by the contract in steward_seams.go
// (StewardTopicsKey / StewardTopicsRecord). Both functions below write
// through ctx.BD.Run("remember", ...) / ctx.BD.RunJSON(..., "memories",
// "--json") directly — the memory-store's storage layer, not the
// learnKey/role-tier machinery (write.go). StewardTopicsKey's bare
// "steward:topics:<hostname>" form carries no hot:/fresh: prefix, so it
// lands as the storage layer's implicit cold/permanent tier the same way a
// bare "<role>:<slug>" learn key does — no promotion/demotion needed for a
// record a steward simply keeps current on every topic-creation call.
package verbs

import (
	"fmt"
	"os"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// publishStewardTopics upserts THIS machine's {briefing, direct} thread refs
// (StewardBriefingThreadPath / StewardDirectThreadPath) into the
// dolt-synced memory store at StewardTopicsKey(os.Hostname()), as a
// StewardTopicsRecord. Called from notify.go's runBriefing/runDirect
// immediately after the LOCAL thread-ref file is persisted on first topic
// creation, so publishing rides existing topic-creation with no new user
// step.
func publishStewardTopics(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("publishStewardTopics: nil context")
	}

	briefing, err := readThreadRefFile(StewardBriefingThreadPath(ctx))
	if err != nil {
		return fmt.Errorf("publishStewardTopics: read briefing thread ref: %w", err)
	}
	direct, err := readThreadRefFile(StewardDirectThreadPath(ctx))
	if err != nil {
		return fmt.Errorf("publishStewardTopics: read direct thread ref: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("publishStewardTopics: hostname: %w", err)
	}

	value, err := StewardTopicsRecord{Briefing: briefing, Direct: direct}.Marshal()
	if err != nil {
		return fmt.Errorf("publishStewardTopics: %w", err)
	}

	if _, err := ctx.BD.Run("remember", "--key="+StewardTopicsKey(hostname), value); err != nil {
		return fmt.Errorf("publishStewardTopics: remember: %w", err)
	}
	return nil
}

// isKnownStewardTopic reports whether threadRef is in the synced union of
// ALL machines' published topic refs AND is not this machine's own local
// briefing/direct ref (i.e. it's owned by another steward) — see
// steward_seams.go's "Synced steward-topics record" section. Consumed by
// relay-gating (agent-teams-5y8a.5) as the peer-topic skip check ahead of
// the bd label query. Fails closed (false) on a nil context, an empty
// threadRef, or any memory-store read/parse error, so a synced-store hiccup
// falls through to the relay's normal routing rather than silently
// swallowing a reply.
func isKnownStewardTopic(ctx *cli.Context, threadRef string) bool {
	if ctx == nil || threadRef == "" {
		return false
	}

	var raw map[string]any
	if err := ctx.BD.RunJSON(&raw, "memories", "--json"); err != nil {
		return false
	}

	known := false
	for key, v := range raw {
		if !strings.HasPrefix(key, stewardTopicsKeyPrefix) {
			continue
		}
		value, ok := v.(string)
		if !ok {
			continue
		}
		rec, err := ParseStewardTopicsRecord(value)
		if err != nil {
			continue
		}
		if threadRef == rec.Briefing || threadRef == rec.Direct {
			known = true
			break
		}
	}
	if !known {
		return false
	}

	// Exclude this machine's own local refs — a topic we own is never
	// "another steward's" topic, regardless of what the synced store says.
	ownBriefing, _ := readThreadRefFile(StewardBriefingThreadPath(ctx))
	ownDirect, _ := readThreadRefFile(StewardDirectThreadPath(ctx))
	if threadRef == ownBriefing || threadRef == ownDirect {
		return false
	}
	return true
}
