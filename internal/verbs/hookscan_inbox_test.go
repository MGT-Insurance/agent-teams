package verbs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func putInboxFiles(t *testing.T, sup string, seqs ...string) {
	t.Helper()
	dir := filepath.Join(sup, "inbox")
	if err := os.MkdirAll(filepath.Join(dir, "read"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range seqs {
		if err := os.WriteFile(filepath.Join(dir, s+".json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A read file must not count.
	if err := os.WriteFile(filepath.Join(dir, "read", "1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHookScanInbox_InsideRunCountsFilesPastTrustedCache(t *testing.T) {
	home, sup := t.TempDir(), t.TempDir()
	t.Setenv("DSP_BEAD", "dsp-run1")
	t.Setenv("DSP_SUP", sup)
	putInboxFiles(t, sup, "5", "6")
	putCache(t, home, "s1", cacheFile{ID: "at-111", CheckedAt: time.Now().Add(-time.Minute), Trusted: true})

	stdout, _, calls := cacheScan(t, home, nil, cachePath1, "--session-id", "s1")
	if calls != 0 {
		t.Errorf("fast path should make no bd call, got %d", calls)
	}
	if want := "id: at-111\nunread: 2\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestHookScanInbox_InsideRunAddsToAtMailOnFullPath(t *testing.T) {
	home, sup := t.TempDir(), t.TempDir()
	t.Setenv("DSP_BEAD", "dsp-run1")
	t.Setenv("DSP_SUP", sup)
	putInboxFiles(t, sup, "5", "6")

	stdout, _, _ := cacheScan(t, home, cacheWithMl, cachePath1, "--session-id", "s2")
	if want := "id: at-111\nunread: 3\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestHookScanInbox_OutsideRunIgnoresFiles(t *testing.T) {
	home, sup := t.TempDir(), t.TempDir()
	t.Setenv("DSP_BEAD", "")
	t.Setenv("DSP_SUP", sup)
	putInboxFiles(t, sup, "5", "6")
	putCache(t, home, "s1", cacheFile{ID: "at-111", CheckedAt: time.Now().Add(-time.Minute), Trusted: true})

	stdout, _, _ := cacheScan(t, home, nil, cachePath1, "--session-id", "s1")
	if want := "id: at-111\nunread: 0\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}
