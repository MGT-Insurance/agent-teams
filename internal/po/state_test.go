package po

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeStore struct {
	m    map[string]string
	puts int
	err  error
}

func newFake() *fakeStore { return &fakeStore{m: map[string]string{}} }

func (f *fakeStore) Put(k, v string) error {
	if f.err != nil {
		return f.err
	}
	f.puts++
	f.m[k] = v
	return nil
}

func (f *fakeStore) List(p string) (map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]string{}
	for k, v := range f.m {
		if strings.HasPrefix(k, p) {
			out[k] = v
		}
	}
	return out, nil
}

var cases = []struct{ suffix, value string }{
	{"claim:at-1", `{"fit":"clear","reason":"r","at":"t","lead":true}`},
	{"said:at-1", `[{"to":"dri","at":"t","gist":"g"}]`},
	{"reading:f1:s1:2026-01-02T03:04:05Z", `{"value":"3","source":"warehouse","note":"n"}`},
	{"view", "# view\nbody"},
}

func TestRoundTripEachKindWithContractKeys(t *testing.T) {
	f := newFake()
	for _, c := range cases {
		if err := Set(f, "pricing", c.suffix, c.value); err != nil {
			t.Fatalf("set %s: %v", c.suffix, err)
		}
		if _, ok := f.m["po:pricing:"+c.suffix]; !ok {
			t.Errorf("key po:pricing:%s not stored; have %v", c.suffix, f.m)
		}
		v, ok, err := Get(f, "pricing", c.suffix)
		if err != nil || !ok || v != c.value {
			t.Errorf("get %s = %q %v %v", c.suffix, v, ok, err)
		}
	}
}

func TestListFiltersByKind(t *testing.T) {
	f := newFake()
	for _, c := range cases {
		_ = Set(f, "pricing", c.suffix, c.value)
	}
	_ = Set(f, "pricing", "claim:at-0", cases[0].value)
	_ = Set(f, "other", "claim:at-9", cases[0].value)
	got, _ := List(f, "pricing", "claim:")
	if !reflect.DeepEqual(got, []string{"claim:at-0", "claim:at-1"}) {
		t.Errorf("claim list = %v", got)
	}
	all, _ := List(f, "pricing", "")
	if len(all) != 5 {
		t.Errorf("all = %v", all)
	}
}

func TestReplayedSetDoesNotWriteTwice(t *testing.T) {
	f := newFake()
	_ = Set(f, "pricing", "view", "v")
	_ = Set(f, "pricing", "view", "v")
	if f.puts != 1 {
		t.Errorf("puts = %d, want 1", f.puts)
	}
	_ = Set(f, "pricing", "view", "v2")
	if f.puts != 2 {
		t.Errorf("changed value must write; puts = %d", f.puts)
	}
}

func TestUnreachable(t *testing.T) {
	f := newFake()
	f.err = ErrUnreachable
	if _, _, err := Get(f, "a", "view"); !errors.Is(err, ErrUnreachable) {
		t.Errorf("get: %v", err)
	}
	if _, err := List(f, "a", ""); !errors.Is(err, ErrUnreachable) {
		t.Errorf("list: %v", err)
	}
	if err := Set(f, "a", "view", "v"); !errors.Is(err, ErrUnreachable) || f.puts != 0 {
		t.Errorf("set: %v puts=%d", err, f.puts)
	}
	if ErrUnreachable.Error() != "po state store unreachable" {
		t.Error("message drifted")
	}
}

func TestValidation(t *testing.T) {
	f := newFake()
	for _, bad := range [][2]string{
		{"claim:", `{}`}, {"bogus", "x"}, {"reading:f:s:notatime", `{}`},
		{"claim:at-1", `{"fit":"maybe"}`}, {"said:at-1", `[{"to":"bob"}]`}, {"claim:at-1", "notjson"},
	} {
		if err := Set(f, "pricing", bad[0], bad[1]); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if err := Set(f, "Bad_Area", "view", "v"); err == nil {
		t.Error("accepted bad area")
	}
	if f.puts != 0 {
		t.Error("invalid input wrote")
	}
}

func TestReservedTierAreasRejected(t *testing.T) {
	for _, a := range []string{"hot", "fresh", "cold", "archived"} {
		if _, err := KeyPrefix(a); err == nil {
			t.Errorf("area %q should be rejected", a)
		}
	}
	if _, err := KeyPrefix("hotel"); err != nil {
		t.Errorf("hotel is a fine area: %v", err)
	}
}
