package verbs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestBDReadsOnlyViaHelpers fails when a non-test file in this package other
// than bd_reads.go passes "memories" or "config", "get" to a call, so a new
// caller cannot silently drop --readonly. Use readMemoriesJSON and
// readConfigValue instead.
func TestBDReadsOnlyViaHelpers(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "bd_reads.go" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			lits := make([]string, len(call.Args))
			for i, arg := range call.Args {
				if bl, ok := arg.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					lits[i], _ = strconv.Unquote(bl.Value)
				}
			}
			for i, s := range lits {
				if s == "memories" || (s == "config" && i+1 < len(lits) && lits[i+1] == "get") {
					t.Errorf("%s: bd %q call outside bd_reads.go; use readMemoriesJSON or readConfigValue",
						fset.Position(call.Pos()), s)
				}
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned no source files")
	}
}
