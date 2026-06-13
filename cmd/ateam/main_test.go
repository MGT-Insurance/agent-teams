package main

import (
	"testing"
)

// TestRunEmptyVerb checks that an empty argv returns exit 2.
func TestRunEmptyVerb(t *testing.T) {
	code := run(nil)
	if code != 2 {
		t.Errorf("run(nil) = %d, want 2", code)
	}
}

// TestRunEmptyVerbString checks that an empty string verb returns exit 2.
func TestRunEmptyVerbString(t *testing.T) {
	code := run([]string{""})
	// empty string is treated same as no verb
	if code != 2 {
		t.Errorf("run([\"\"]) = %d, want 2", code)
	}
}
