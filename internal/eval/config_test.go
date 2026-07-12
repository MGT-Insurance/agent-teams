package eval

import "testing"

func TestConfigFingerprintHash_Stable(t *testing.T) {
	c := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus", Advisor: ""}
	h1 := c.Hash()
	h2 := c.Hash()
	if h1 != h2 {
		t.Fatalf("Hash() not stable: %q != %q", h1, h2)
	}
	if len(h1) != 12 {
		t.Fatalf("Hash() length = %d, want 12", len(h1))
	}
}

func TestConfigFingerprintHash_DiffersOnFieldChange(t *testing.T) {
	a := ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus", Advisor: ""}
	b := ConfigFingerprint{Name: "sonnet-advisor", DRIModel: "sonnet", Advisor: "opus"}
	if a.Hash() == b.Hash() {
		t.Fatalf("distinct configs hashed identically: %q", a.Hash())
	}
}

func TestConfigFingerprintHash_CanonicalOverMapKeyOrder(t *testing.T) {
	// encoding/json sorts map keys, so insertion order into PerRoleModels
	// must not affect the hash.
	a := ConfigFingerprint{PerRoleModels: map[string]string{"dri": "opus", "reviewer": "sonnet"}}
	b := ConfigFingerprint{PerRoleModels: map[string]string{"reviewer": "sonnet", "dri": "opus"}}
	if a.Hash() != b.Hash() {
		t.Fatalf("Hash() not canonical over map key order: %q != %q", a.Hash(), b.Hash())
	}
}
