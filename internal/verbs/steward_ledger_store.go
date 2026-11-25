package verbs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/po"
)

// stewardLedgerKeyPrefix starts every hosted ledger memory key. The keys carry
// no colon: colon keys are read as role learnings.
const stewardLedgerKeyPrefix = "stewardledger."

// stewardLedgerStore is where ledger records live: the local JSONL file, or
// one bd memory per record in the hosted store.
type stewardLedgerStore interface {
	// append stores one record. line is the record's JSON without a trailing
	// newline.
	append(rec StewardLedgerRecord, line []byte) error
	// lines returns the stored record lines, oldest first. verb names the
	// calling verb in errors. A store with no records returns none.
	lines(verb string) ([]string, error)
}

// hostedStewardLedger returns the hosted store client and the store name for
// error messages, or ok false when the ledger is a local file. The ledger is
// hosted when the home is a hosted workspace or cloud_home routes learnings to
// another workspace.
func hostedStewardLedger(ctx *cli.Context) (client cli.BDRunner, store string, ok bool) {
	host, hosted := bd.HostedServer(ctx.Home)
	remote := learnRemote(ctx)
	if !hosted && !remote {
		return nil, "", false
	}
	if remote {
		return learnBD(ctx), learnStoreName(ctx), true
	}
	return learnBD(ctx), host, true
}

// newStewardLedgerStore selects the steward's ledger store for ctx.
func newStewardLedgerStore(ctx *cli.Context) stewardLedgerStore {
	store, _ := newPersonaLedgerStore(ctx, StewardLedgerPersonaSteward)
	return store
}

// stewardLedgerPOPersonaPrefix starts every PO persona name; the rest is the
// PO's kebab-case area.
const stewardLedgerPOPersonaPrefix = "po-"

// stewardLedgerAreaOK reports whether area is non-empty kebab-case
// (lowercase letters and digits joined by single hyphens).
func stewardLedgerAreaOK(area string) bool {
	if area == "" || strings.HasPrefix(area, "-") || strings.HasSuffix(area, "-") || strings.Contains(area, "--") {
		return false
	}
	for _, r := range area {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// stewardLedgerPersonaKeyPrefix returns the hosted key prefix for persona:
// "stewardledger." for the steward, "poledger.<area>." for po-<area>. It
// errors on any other persona name.
func stewardLedgerPersonaKeyPrefix(persona string) (string, error) {
	if persona == StewardLedgerPersonaSteward {
		return stewardLedgerKeyPrefix, nil
	}
	if area, ok := strings.CutPrefix(persona, stewardLedgerPOPersonaPrefix); ok && stewardLedgerAreaOK(area) {
		return po.LedgerPrefix + area + ".", nil
	}
	return "", fmt.Errorf("invalid persona %q (want steward or po-<area>)", persona)
}

// newPersonaLedgerStore selects the ledger store for persona on ctx. The
// steward persona gets the steward's store unchanged; a po-<area> persona gets
// its own file (ledger-po-<area>.jsonl beside the steward's) or its own
// poledger.<area>. key prefix, so the two never read each other's rows.
func newPersonaLedgerStore(ctx *cli.Context, persona string) (stewardLedgerStore, error) {
	prefix, err := stewardLedgerPersonaKeyPrefix(persona)
	if err != nil {
		return nil, err
	}
	if client, store, ok := hostedStewardLedger(ctx); ok {
		return &hostedLedgerStore{client: client, store: store, prefix: prefix}, nil
	}
	path := StewardLedgerPath(ctx)
	if persona != StewardLedgerPersonaSteward {
		path = filepath.Join(StewardHome(ctx), "ledger-"+persona+".jsonl")
	}
	return &fileLedgerStore{path: path}, nil
}

// fileLedgerStore keeps the ledger in one JSONL file.
type fileLedgerStore struct{ path string }

func (s *fileLedgerStore) append(_ StewardLedgerRecord, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("ateam steward ledger record: create ledger dir: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("ateam steward ledger record: open ledger: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(append([]byte{}, line...), '\n')); err != nil {
		return fmt.Errorf("ateam steward ledger record: append: %w", err)
	}
	return nil
}

func (s *fileLedgerStore) lines(verb string) ([]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("ateam steward ledger %s: read ledger: %w", verb, err)
	}
	return strings.Split(string(data), "\n"), nil
}

// hostedLedgerStore keeps one bd memory per record.
type hostedLedgerStore struct {
	client cli.BDRunner
	store  string
	// prefix is the key prefix of this persona's rows; empty means the
	// steward's (stewardLedgerKeyPrefix).
	prefix string
}

func (s *hostedLedgerStore) keyPrefix() string {
	if s.prefix == "" {
		return stewardLedgerKeyPrefix
	}
	return s.prefix
}

// stewardLedgerKey returns the hosted memory key for a record with timestamp
// ts (unix nanoseconds) and JSON value.
func stewardLedgerKey(rec StewardLedgerRecord, value string) string {
	return stewardLedgerKeyWithPrefix(stewardLedgerKeyPrefix, rec, value)
}

func stewardLedgerKeyWithPrefix(prefix string, rec StewardLedgerRecord, value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s%019d-%s", prefix, rec.Timestamp.UnixNano(), hex.EncodeToString(sum[:])[:4])
}

func (s *hostedLedgerStore) append(rec StewardLedgerRecord, line []byte) error {
	value := string(line)
	if _, err := s.client.Run("remember", "--key="+stewardLedgerKeyWithPrefix(s.keyPrefix(), rec, value), value); err != nil {
		if isStoreUnreachable(err) {
			return fmt.Errorf("ateam steward ledger record: ledger store unreachable (%s): may not be saved; retry later", s.store)
		}
		return fmt.Errorf("ateam steward ledger record: %w", err)
	}
	return nil
}

// rows returns the ledger memories keyed by memory key.
func (s *hostedLedgerStore) rows(verb string) (map[string]string, error) {
	raw, err := readMemoriesByPrefix(s.client, s.keyPrefix())
	if err != nil {
		if isStoreUnreachable(err) {
			return nil, fmt.Errorf("ateam steward ledger %s: ledger store unreachable (%s)", verb, s.store)
		}
		return nil, fmt.Errorf("ateam steward ledger %s: read ledger: %w", verb, err)
	}
	out := map[string]string{}
	for k, v := range raw {
		if sv, ok := v.(string); ok && strings.HasPrefix(k, s.keyPrefix()) {
			out[k] = sv
		}
	}
	return out, nil
}

func (s *hostedLedgerStore) lines(verb string) ([]string, error) {
	rows, err := s.rows(verb)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, rows[k])
	}
	return out, nil
}

// ── steward ledger seed ──────────────────────────────────────────────────────

// stewardLedgerSeedKong is the kong struct for `ateam steward ledger seed`.
type stewardLedgerSeedKong struct {
	DryRun bool `name:"dry-run" help:"Report what would be copied without writing."`
}

// Run copies the local ledger file into the hosted store in one bd import. It
// skips records already present, never overwrites, and never changes the file.
func (c *stewardLedgerSeedKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam steward ledger seed: nil context")
	}
	client, storeName, ok := hostedStewardLedger(ctx)
	if !ok {
		return fmt.Errorf("ateam steward ledger seed: no hosted ledger store (set cloud_home)")
	}
	store := &hostedLedgerStore{client: client, store: storeName}

	data, err := os.ReadFile(StewardLedgerPath(ctx))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ateam steward ledger seed: read ledger: %w", err)
	}
	present, err := store.rows("seed")
	if err != nil {
		return err
	}

	var missing []seedMemoryLine
	alreadyPresent, malformed := 0, 0
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rec, err := ParseStewardLedgerRecord([]byte(line))
		if err != nil {
			malformed++
			continue
		}
		value, err := json.Marshal(rec)
		if err != nil {
			malformed++
			continue
		}
		key := stewardLedgerKey(rec, string(value))
		if _, ok := present[key]; ok || seen[key] {
			alreadyPresent++
			continue
		}
		seen[key] = true
		missing = append(missing, seedMemoryLine{Type: "memory", Key: key, Value: string(value)})
	}

	verb := "to write"
	if c.DryRun {
		verb = "would write"
	}
	summary := fmt.Sprintf("seed: %d %s, %d already present, %d malformed skipped\n", len(missing), verb, alreadyPresent, malformed)
	if c.DryRun || len(missing) == 0 {
		fmt.Fprint(ctx.Stdout, summary)
		return nil
	}

	path, err := writeSeedJSONL(missing)
	if err != nil {
		return fmt.Errorf("ateam steward ledger seed: %w", err)
	}
	defer os.Remove(path)
	if _, err := client.Run("import", path); err != nil {
		if isStoreUnreachable(err) {
			return fmt.Errorf("ateam steward ledger seed: ledger store unreachable (%s): may not be saved; retry later", storeName)
		}
		return fmt.Errorf("ateam steward ledger seed: %w", err)
	}
	// A real run reports its count only after the import succeeds.
	fmt.Fprint(ctx.Stdout, summary)
	return nil
}
