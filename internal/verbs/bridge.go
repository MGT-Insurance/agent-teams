package verbs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

// This file holds the frozen names and pure helpers of the cloud-laptop mail
// bridge. Three tracks build on them: laptop-to-cloud send (mail_cloud_send.go),
// cloud-to-laptop bridge beads (mail_bridge_send.go), and the relay pickup.

// LocalStewardHandle is the cloud-side address of the laptop steward.
const LocalStewardHandle = "local-steward"

// bridgeLabel marks a hosted message bead that waits for laptop pickup.
const bridgeLabel = "bridge:laptop"

// bridgeDeliveredLabel marks a hosted message bead the laptop has delivered.
const bridgeDeliveredLabel = "delivery:bridged"

// bridgeDedupKey returns the --dedup-key the laptop passes to its local send
// when it delivers the hosted message hostedID.
func bridgeDedupKey(hostedID string) string {
	return "bridge-" + hostedID
}

// bridgeCloudHome resolves the hosted workspace the bridge talks to. It
// returns ok=false with a nil error when cloud_home is unset or names the same
// workspace as home. Every bridge path calls it first; ok=false means no-op.
func bridgeCloudHome(home string) (cloudHome string, ok bool, err error) {
	cloudHome, ok, err = workspaceconfig.CloudHome(home)
	if err != nil || !ok {
		return "", false, err
	}
	if sameWorkspace(home, cloudHome) {
		return "", false, nil
	}
	return cloudHome, true, nil
}

// cloudCommentAuthor returns the actor a laptop-to-cloud comment is written
// as. Dispatcher's wake path ignores authors that start with "job:" or
// "system:" and the author "reconcile"; none of the results do.
func cloudCommentAuthor(sender string) string {
	if peerInitiativeID.MatchString(sender) {
		return "initiative:" + sender
	}
	return "laptop:" + sender
}

// cloudReplyHandle returns the handle a cloud run uses to reply to sender.
func cloudReplyHandle(sender string) string {
	if sender == StewardHandle {
		return LocalStewardHandle
	}
	return sender
}

// cloudCommentText formats a laptop-to-cloud message the way Dispatcher
// formats peer mail, so the cloud DRI reads one format. The trailing mail-key
// line lets a retry detect that the comment already exists.
func cloudCommentText(from, body, key string) string {
	return fmt.Sprintf("message from %s:\n%s\n\nReply with: ateam mail send %s --file <file>\nmail-key: %s",
		from, body, cloudReplyHandle(from), key)
}

// laptopMailKey returns the idempotency key for one laptop message: the host
// plus the first 12 hex characters of sha256(sender + "\n" + body).
func laptopMailKey(host, sender string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(sender + "\n"))
	h.Write(body)
	return "laptop:" + host + ":" + hex.EncodeToString(h.Sum(nil))[:12]
}

// CloudStewardHandle is the laptop-side address of the Dispatcher steward agent.
const CloudStewardHandle = "cloud-steward"

// cloudStewardAssignee is the assignee of a mail bead the Dispatcher wakes the
// steward agent for: the Dispatcher wake prefix "agent:" plus the agent name.
const cloudStewardAssignee = "agent:steward"

// stewardMessageKind is the kind: header of a steward-to-steward mail body.
const stewardMessageKind = "steward-message"

// mailKeyLabelPrefix prefixes the label on a Dispatcher mail bead that carries
// the mail key, used for the cloudDedupWindow dedup window.
const mailKeyLabelPrefix = "mail-key:"

// cloudStewardMailTitle returns the title of a mail bead from the sender.
func cloudStewardMailTitle(from string) string {
	return from + ": message"
}

// cloudStewardMailBody formats a steward-to-steward mail body. The mail-key
// line lets a retry detect that the mail already exists.
func cloudStewardMailBody(from, body, key string) string {
	return "from: " + from + "\nkind: " + stewardMessageKind + "\n---\n" + body +
		"\n\nReply with: ateam mail send " + from + " --body <text>\nmail-key: " + key
}
