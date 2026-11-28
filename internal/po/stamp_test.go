package po

import (
	"strings"
	"testing"
)

func TestStampRoundTrip(t *testing.T) {
	for _, ext := range []bool{false, true} {
		s := FormatStamp("eric.lloyd", "po-doors-of-durin", ext)
		if !strings.HasPrefix(s, CommentPrefix) {
			t.Fatalf("stamp %q lacks CommentPrefix", s)
		}
		o, a, e, ok := ParseStamp(s)
		if !ok || o != "eric.lloyd" || a != "po-doors-of-durin" || e != ext {
			t.Fatalf("round trip ext=%v got %q %q %v %v", ext, o, a, e, ok)
		}
	}
	want := "po: note (not Eric's words): from joe/po-x (external)"
	if got := FormatStamp("joe", "po-x", true); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseStampRejects(t *testing.T) {
	p := CommentPrefix
	bad := []string{
		"",
		p,                                   // bare legacy prefix
		p + " free text from a legacy note", // legacy with body
		p + " from joe/po-x",                // no kind
		p + " from joe/po-x (other)",
		p + " from joe/po-x (Internal)",
		p + " from joe (internal)",
		p + " from /po-x (internal)",
		p + " from joe/ (internal)",
		p + " from Joe/po-x (internal)",
		p + " from joe/po x (internal)",
		p + " from joe/po-x/y (internal)",
		p + " from joe/po-x (internal)\nextra",
		p + " from joe\n/po-x (internal)",
		p + " from joe/po-x) (external) (internal)",
		p + " from joe/po-x (external) (internal)",
		p + " from joe/po-x (internal) ",
		p + " from -joe/po-x (internal)",
		"x" + p + " from joe/po-x (internal)",
		p + " from joe/po-x (internal)\r",
		p + " from joe/po-x\t(internal)",
		p + " from joe/po\r-x (internal)",
		p + " from joe/po-x\n (internal)",
		p + " from a/b/c (internal)",
	}
	for _, s := range bad {
		if _, _, _, ok := ParseStamp(s); ok {
			t.Errorf("ParseStamp(%q) ok, want false", s)
		}
	}
}

func TestLegacyNoteStillMatchesPrefix(t *testing.T) {
	legacy := CommentPrefix + " consider the onboarding flow"
	if !strings.HasPrefix(legacy, CommentPrefix) {
		t.Fatal("legacy note lost prefix")
	}
	if _, _, _, ok := ParseStamp(legacy); ok {
		t.Fatal("legacy note must parse as unknown source")
	}
}

func TestFormatStampRefusesInjection(t *testing.T) {
	cases := [][2]string{
		{"joe\nrelayed-by: x", "po"},
		{"joe", "po\n"},
		{"joe) (internal", "po"},
		{"joe", "po)"},
		{"joe/x", "po"},
		{"", "po"},
		{"joe", ""},
		{"Joe", "po"},
		{"joe", "po x"},
	}
	for _, c := range cases {
		if s := FormatStamp(c[0], c[1], false); s != "" {
			t.Errorf("FormatStamp(%q,%q) = %q, want empty", c[0], c[1], s)
		}
	}
}

func TestStampConstants(t *testing.T) {
	if CommentPrefix != "po: note (not Eric's words):" {
		t.Fatal("CommentPrefix changed")
	}
	for _, c := range []string{RelayedByPrefix, ValueCheckFits, ValueCheckDoesNotFit, RequestChangesDrift, RequestChangesOneWayDoor} {
		if c == "" {
			t.Fatal("empty constant")
		}
	}
}

func TestParseStampSplitsOwnerAndAgent(t *testing.T) {
	o, a, ext, ok := ParseStamp(CommentPrefix + " from eric.lloyd/po-a.b-c (external)")
	if !ok || o != "eric.lloyd" || a != "po-a.b-c" || !ext {
		t.Fatalf("got %q %q %v %v", o, a, ext, ok)
	}
	if _, _, ext, ok := ParseStamp(CommentPrefix + " from o/a (internal)"); !ok || ext {
		t.Fatalf("internal parsed as external: %v %v", ext, ok)
	}
}
