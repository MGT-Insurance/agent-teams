package po

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Kinds of per-PO state; each owns one key family from doc.go section 3.
const (
	KindClaim   = "claim"
	KindSaid    = "said"
	KindReading = "reading"
	KindView    = "view"
)

// ErrUnreachable is returned when the hosted store cannot be reached. Its text
// is the message the verbs print.
var ErrUnreachable = errors.New("po state store unreachable")

// Store is the hosted-memory seam the state functions run over. The verbs
// package backs it with the same bd memory client learnings and the steward
// ledger use; tests back it with a fake.
type Store interface {
	// Put writes value at key, replacing any earlier value.
	Put(key, value string) error
	// List returns every key starting with prefix and its value.
	List(prefix string) (map[string]string, error)
}

var (
	areaRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	idRe   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// reservedAreas are the learnings tier names. A key po:<tier>:<slug> is a real
// learning of the shared "po" role (for example po:hot:view), so no PO area may
// take one of these names or its state keys would collide with learnings.
var reservedAreas = map[string]bool{"hot": true, "fresh": true, "cold": true, "archived": true}

// ValidateArea checks that area is kebab-case and not a reserved tier name.
func ValidateArea(area string) error {
	if !areaRe.MatchString(area) {
		return fmt.Errorf("invalid area %q: want kebab-case", area)
	}
	if reservedAreas[area] {
		return fmt.Errorf("invalid area %q: reserved (learnings tier name)", area)
	}
	return nil
}

// IsReservedArea reports whether name is a learnings tier word.
func IsReservedArea(name string) bool { return reservedAreas[name] }

// KeyPrefix returns "po:<area>:", the prefix of every key a PO owns.
func KeyPrefix(area string) (string, error) {
	if err := ValidateArea(area); err != nil {
		return "", err
	}
	return "po:" + area + ":", nil
}

// KindOf returns the kind of a key suffix (the part after po:<area>:), or ""
// when the suffix is not in the contract key scheme.
func KindOf(suffix string) string {
	if suffix == KindView {
		return KindView
	}
	for _, k := range []string{KindClaim, KindSaid, KindReading} {
		if strings.HasPrefix(suffix, k+":") {
			return k
		}
	}
	return ""
}

// ValidateSuffix checks a key suffix against the contract key scheme:
//
//	view
//	claim:<initiative-id>
//	said:<initiative-id>
//	reading:<feature-id>:<signal-id>:<rfc3339>
func ValidateSuffix(suffix string) error {
	switch KindOf(suffix) {
	case KindView:
		return nil
	case KindClaim, KindSaid:
		_, id, _ := strings.Cut(suffix, ":")
		if !idRe.MatchString(id) {
			return fmt.Errorf("invalid key %q: want %s:<initiative-id>", suffix, KindOf(suffix))
		}
		return nil
	case KindReading:
		parts := strings.SplitN(suffix, ":", 4)
		if len(parts) != 4 || !idRe.MatchString(parts[1]) || !idRe.MatchString(parts[2]) {
			return fmt.Errorf("invalid key %q: want reading:<feature-id>:<signal-id>:<rfc3339>", suffix)
		}
		if _, err := time.Parse(time.RFC3339, parts[3]); err != nil {
			return fmt.Errorf("invalid key %q: timestamp is not rfc3339", suffix)
		}
		return nil
	}
	return fmt.Errorf("invalid key %q: want view, claim:<id>, said:<id> or reading:<feature>:<signal>:<rfc3339>", suffix)
}

// ValidateValue checks that value has the shape its key's kind requires.
func ValidateValue(suffix, value string) error {
	switch KindOf(suffix) {
	case KindClaim:
		var c Claim
		if err := json.Unmarshal([]byte(value), &c); err != nil {
			return fmt.Errorf("claim value is not a JSON claim: %w", err)
		}
		if c.Fit != "clear" && c.Fit != "possible" {
			return fmt.Errorf("claim fit %q: want clear or possible", c.Fit)
		}
	case KindSaid:
		var s []Said
		if err := json.Unmarshal([]byte(value), &s); err != nil {
			return fmt.Errorf("said value is not a JSON list of said entries: %w", err)
		}
		for _, e := range s {
			if e.To != "dri" && e.To != "steward" && e.To != "eric" {
				return fmt.Errorf("said to %q: want dri, steward or eric", e.To)
			}
		}
	case KindReading:
		var r Reading
		if err := json.Unmarshal([]byte(value), &r); err != nil {
			return fmt.Errorf("reading value is not a JSON reading: %w", err)
		}
	}
	return nil
}

// Set writes value at po:<area>:<suffix>. Writing the value already stored is
// a no-op, so a replayed wake does not write twice. A failed write returns an
// error and queues nothing.
func Set(s Store, area, suffix, value string) error {
	prefix, err := KeyPrefix(area)
	if err != nil {
		return err
	}
	if err := ValidateSuffix(suffix); err != nil {
		return err
	}
	if err := ValidateValue(suffix, value); err != nil {
		return err
	}
	key := prefix + suffix
	cur, err := s.List(key)
	if err != nil {
		return err
	}
	if old, ok := cur[key]; ok && old == value {
		return nil
	}
	return s.Put(key, value)
}

// Get returns the value at po:<area>:<suffix>. ok is false when none is stored.
func Get(s Store, area, suffix string) (value string, ok bool, err error) {
	prefix, err := KeyPrefix(area)
	if err != nil {
		return "", false, err
	}
	if err := ValidateSuffix(suffix); err != nil {
		return "", false, err
	}
	key := prefix + suffix
	rows, err := s.List(key)
	if err != nil {
		return "", false, err
	}
	value, ok = rows[key]
	return value, ok, nil
}

// List returns the key suffixes of area whose suffix starts with p, sorted.
// p "claim:" lists one kind.
func List(s Store, area, p string) ([]string, error) {
	prefix, err := KeyPrefix(area)
	if err != nil {
		return nil, err
	}
	rows, err := s.List(prefix + p)
	if err != nil {
		return nil, err
	}
	var out []string
	for k := range rows {
		if strings.HasPrefix(k, prefix+p) {
			out = append(out, strings.TrimPrefix(k, prefix))
		}
	}
	sort.Strings(out)
	return out, nil
}
