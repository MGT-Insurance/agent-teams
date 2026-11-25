package verbs

import (
	"strings"
	"testing"
)

func TestPoStubsNotImplemented(t *testing.T) {
	for _, err := range []error{
		poCharterCheckRun(nil, &poCharterCheckKong{}),
		poStateGetRun(nil, &poStateGetKong{}),
		poStateSetRun(nil, &poStateSetKong{}),
		poStateListRun(nil, &poStateListKong{}),
		poStartupRun(nil, &poStartupKong{}),
	} {
		if err == nil || !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("stub error = %v, want not implemented", err)
		}
	}
}
