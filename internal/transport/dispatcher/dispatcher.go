// Package dispatcher is the transport a cloud run uses to reach the human. It
// sends the same posts and topic closes the telegram transport does, but
// through the Dispatcher run-tools route (POST /api/run-tools), so the sandbox
// holds no Telegram token. Select it with AGENT_TEAMS_TRANSPORT=dispatcher
// inside a Dispatcher run.
package dispatcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/runtools"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// maxTitleRunes is the longest topic title the run-tools route accepts.
const maxTitleRunes = 500

// requestTimeout bounds each HTTP request to Dispatcher, so a stalled
// connection makes Send and CloseTopic return an error instead of blocking.
const requestTimeout = 45 * time.Second

// clientTimeout is requestTimeout; tests shorten it.
var clientTimeout = requestTimeout

// Dispatcher posts to and closes the run's own initiative topic through
// Dispatcher.
type Dispatcher struct {
	client *runtools.Client
	do     runtools.Do
}

// Factory builds the transport. It needs DSP_BEAD and DSP_API_URL, which only
// a Dispatcher run sets.
func Factory(home string) (transport.Transport, error) {
	c, ok := runtools.FromEnv()
	if !ok {
		return nil, errors.New("dispatcher transport: DSP_BEAD and DSP_API_URL must be set (only inside a Dispatcher run)")
	}
	return &Dispatcher{client: c, do: (&http.Client{Timeout: clientTimeout}).Do}, nil
}

// Name returns "dispatcher".
func (d *Dispatcher) Name() string { return "dispatcher" }

type postRequest struct {
	Op         string `json:"op"`
	Initiative string `json:"initiative"`
	Title      string `json:"title,omitempty"`
	Text       string `json:"text"`
}

// Send posts msg to the run's initiative topic and returns the topic's
// thread_ref. It makes one attempt, because a post is not idempotent. msg's
// ThreadRef is never sent: Dispatcher resolves the topic from the run.
func (d *Dispatcher) Send(msg transport.OutboundMessage) (string, error) {
	if msg.General || msg.ChatRef != "" {
		return "", errors.New("dispatcher transport: a cloud run posts only to its own initiative's topic")
	}
	if msg.ImagePath != "" || msg.DocumentPath != "" {
		return "", errors.New("dispatcher transport: attachments are not supported")
	}
	title := msg.Title
	if r := []rune(title); len(r) > maxTitleRunes {
		title = string(r[:maxTitleRunes])
	}
	data, err := d.client.Call(d.do, "telegram_post", postRequest{
		Op:         "telegram_post",
		Initiative: msg.InitiativeID,
		Title:      title,
		Text:       msg.Body,
	}, 0)
	if err != nil {
		return "", err
	}
	var r struct {
		ThreadRef string `json:"thread_ref"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("run-tools telegram_post: decode: %w", err)
	}
	return r.ThreadRef, nil
}

// CloseTopic closes the run's initiative topic. Closing is idempotent, so it
// retries once after a network error or a 5xx.
func (d *Dispatcher) CloseTopic(threadRef string) error {
	_, err := d.client.Call(d.do, "telegram_close", map[string]string{
		"op":         "telegram_close",
		"thread_ref": threadRef,
	}, 1)
	return err
}

// Receive returns at once: inbound Telegram reaches the cloud steward, not the
// run.
func (d *Dispatcher) Receive(handler func(transport.Reply) error) error {
	return errors.New("dispatcher transport does not receive; inbound Telegram reaches the cloud steward")
}
