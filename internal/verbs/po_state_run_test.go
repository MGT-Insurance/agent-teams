package verbs

import (
	"errors"
	"strings"
	"testing"
)

func TestPoStateVerbsRoundTripHosted(t *testing.T) {
	m := newMemBD()
	ctx, out, _ := remoteLedgerCtx(t, m)
	if err := poStateSetRun(ctx, &poStateSetKong{Area: "pricing", Key: "view", Value: "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(m.remember) != 1 || m.remember[0][1] != "--key=po:pricing:view" {
		t.Fatalf("remember = %v", m.remember)
	}
	// replay: no second write
	if err := poStateSetRun(ctx, &poStateSetKong{Area: "pricing", Key: "view", Value: "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(m.remember) != 1 {
		t.Fatalf("replay wrote again: %v", m.remember)
	}
	out.Reset()
	if err := poStateGetRun(ctx, &poStateGetKong{Area: "pricing", Key: "view"}); err != nil || out.String() != "hello\n" {
		t.Fatalf("get = %q %v", out.String(), err)
	}
	out.Reset()
	if err := poStateListRun(ctx, &poStateListKong{Area: "pricing"}); err != nil || out.String() != "view\n" {
		t.Fatalf("list = %q %v", out.String(), err)
	}
}

func TestPoStateVerbsUnreachable(t *testing.T) {
	m := newMemBD()
	m.err = errors.New("Dolt server unreachable")
	ctx, _, _ := remoteLedgerCtx(t, m)
	for _, err := range []error{
		poStateGetRun(ctx, &poStateGetKong{Area: "a", Key: "view"}),
		poStateListRun(ctx, &poStateListKong{Area: "a"}),
		poStateSetRun(ctx, &poStateSetKong{Area: "a", Key: "view", Value: "v"}),
	} {
		if err == nil || !strings.Contains(err.Error(), "po state store unreachable") {
			t.Errorf("err = %v", err)
		}
	}
}
