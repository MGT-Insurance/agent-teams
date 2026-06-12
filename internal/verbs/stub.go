// Contract-owned shared stub. No track edits this. Integration (agent-teams-w6t)
// deletes it once every track implements real verbs and no file calls stub() anymore.
package verbs

import (
	"fmt"

	"github.com/erlloyd/agent-teams/internal/cli"
)

type stubCommand struct {
	name string
}

func stub(name string) *stubCommand { return &stubCommand{name: name} }

func (s *stubCommand) Name() string { return s.name }

func (s *stubCommand) Run(_ *cli.Context, _ []string) error {
	return fmt.Errorf("ateam %s: not implemented", s.name)
}
