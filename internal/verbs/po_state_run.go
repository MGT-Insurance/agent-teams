package verbs

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/po"
)

// poStateStore backs po.Store with the learnings bd memory client, the same
// hosted store the steward ledger uses.
type poStateStore struct{ client cli.BDRunner }

func (s poStateStore) Put(key, value string) error {
	if _, err := s.client.Run("remember", "--key="+key, value); err != nil {
		if isStoreUnreachable(err) {
			return po.ErrUnreachable
		}
		return err
	}
	return nil
}

func (s poStateStore) List(prefix string) (map[string]string, error) {
	raw, err := readMemoriesByPrefix(s.client, prefix)
	if err != nil {
		if isStoreUnreachable(err) {
			return nil, po.ErrUnreachable
		}
		return nil, err
	}
	out := map[string]string{}
	for k, v := range raw {
		if sv, ok := v.(string); ok && strings.HasPrefix(k, prefix) {
			out[k] = sv
		}
	}
	return out, nil
}

func poStateStoreOf(ctx *cli.Context, verb string) (po.Store, error) {
	if ctx == nil || learnBD(ctx) == nil {
		return nil, fmt.Errorf("ateam po state %s: no store client", verb)
	}
	return poStateStore{client: learnBD(ctx)}, nil
}

func poStateGetRun(ctx *cli.Context, c *poStateGetKong) error {
	s, err := poStateStoreOf(ctx, "get")
	if err != nil {
		return err
	}
	v, ok, err := po.Get(s, c.Area, c.Key)
	if err != nil {
		return fmt.Errorf("ateam po state get: %w", err)
	}
	if !ok {
		return fmt.Errorf("ateam po state get: no value at po:%s:%s", c.Area, c.Key)
	}
	if !strings.HasSuffix(v, "\n") {
		v += "\n"
	}
	_, err = io.WriteString(ctx.Stdout, v)
	return err
}

func poStateSetRun(ctx *cli.Context, c *poStateSetKong) error {
	if (c.File == "") == (c.Value == "") {
		return fmt.Errorf("ateam po state set: give exactly one of --file or --value")
	}
	value := c.Value
	if c.File != "" {
		var data []byte
		var err error
		if c.File == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(c.File)
		}
		if err != nil {
			return fmt.Errorf("ateam po state set: read value: %w", err)
		}
		value = string(data)
	}
	s, err := poStateStoreOf(ctx, "set")
	if err != nil {
		return err
	}
	if err := po.Set(s, c.Area, c.Key, value); err != nil {
		return fmt.Errorf("ateam po state set: %w", err)
	}
	fmt.Fprintf(ctx.Stdout, "po:%s:%s\n", c.Area, c.Key)
	return nil
}

func poStateListRun(ctx *cli.Context, c *poStateListKong) error {
	s, err := poStateStoreOf(ctx, "list")
	if err != nil {
		return err
	}
	keys, err := po.List(s, c.Area, c.Prefix)
	if err != nil {
		return fmt.Errorf("ateam po state list: %w", err)
	}
	for _, k := range keys {
		fmt.Fprintln(ctx.Stdout, k)
	}
	return nil
}
