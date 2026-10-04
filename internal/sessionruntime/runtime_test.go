package sessionruntime

import (
	"fmt"
	"testing"
)

func TestResolveNew(t *testing.T) {
	tests := []struct {
		name, explicit, machine, configured string
		configSet                           bool
		configErr                           error
		want                                Kind
		wantErr                             bool
		wantConfigCall                      bool
	}{
		{name: "legacy default", want: Claude, wantConfigCall: true},
		{name: "machine default", machine: "codex", want: Codex},
		{name: "auto machine default", explicit: "auto", machine: "codex", want: Codex},
		{name: "config default", configured: "codex", configSet: true, want: Codex, wantConfigCall: true},
		{name: "auto config default", explicit: "auto", configured: "codex", configSet: true, want: Codex, wantConfigCall: true},
		{name: "missing config key", want: Claude, wantConfigCall: true},
		{name: "explicit wins lazily", explicit: "claude", machine: "codex", configured: "codex", configSet: true, configErr: fmt.Errorf("must not be read"), want: Claude},
		{name: "machine wins lazily", machine: "codex", configured: "claude", configSet: true, configErr: fmt.Errorf("must not be read"), want: Codex},
		{name: "invalid explicit", explicit: "other", wantErr: true},
		{name: "invalid machine", explicit: "auto", machine: "other", wantErr: true},
		{name: "config error", configErr: fmt.Errorf("bad config"), wantErr: true, wantConfigCall: true},
		{name: "invalid config", configured: "other", configSet: true, wantErr: true, wantConfigCall: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configCalled := false
			got, err := ResolveNew(tt.explicit, tt.machine, func() (string, bool, error) {
				configCalled = true
				return tt.configured, tt.configSet, tt.configErr
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveNew() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ResolveNew() = %q, want %q", got, tt.want)
			}
			if configCalled != tt.wantConfigCall {
				t.Fatalf("config resolver called = %v, want %v", configCalled, tt.wantConfigCall)
			}
		})
	}
}

func TestResolveStoredAndAssertion(t *testing.T) {
	if got, err := ResolveStored(""); err != nil || got != Claude {
		t.Fatalf("legacy ResolveStored = %q, %v", got, err)
	}
	if _, err := ResolveStored("unknown"); err == nil {
		t.Fatal("unknown stored runtime must fail closed")
	}
	if got, err := AssertStored("codex", "codex"); err != nil || got != Codex {
		t.Fatalf("matching assertion = %q, %v", got, err)
	}
	if _, err := AssertStored("codex", "claude"); err == nil {
		t.Fatal("mismatched assertion must fail")
	}
}
