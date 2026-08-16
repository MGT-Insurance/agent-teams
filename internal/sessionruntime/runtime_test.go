package sessionruntime

import "testing"

func TestResolveNew(t *testing.T) {
	tests := []struct {
		name, explicit, machine string
		want                    Kind
		wantErr                 bool
	}{
		{name: "legacy default", want: Claude},
		{name: "machine default", machine: "codex", want: Codex},
		{name: "auto machine default", explicit: "auto", machine: "codex", want: Codex},
		{name: "explicit wins", explicit: "claude", machine: "codex", want: Claude},
		{name: "invalid explicit", explicit: "other", wantErr: true},
		{name: "invalid machine", explicit: "auto", machine: "other", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveNew(tt.explicit, tt.machine)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveNew() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ResolveNew() = %q, want %q", got, tt.want)
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
