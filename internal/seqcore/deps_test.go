package seqcore_test

import (
	"go/build"
	"strings"
	"testing"
)

// TestNoRuntimeDependencies asserts that the non-test build of goq imports
// nothing outside the standard library. It is the executable form of the
// zero-runtime-dependency promise.
func TestNoRuntimeDependencies(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"../..", "."} {
		pkg, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("ImportDir(%q): %v", dir, err)
		}
		for _, imp := range pkg.Imports { // Imports excludes _test.go files
			if strings.HasPrefix(imp, "github.com/oleexo/goq") {
				continue // our own internal packages are fine
			}
			if strings.Contains(strings.SplitN(imp, "/", 2)[0], ".") {
				t.Errorf("%s imports non-stdlib package %q", pkg.Name, imp)
			}
		}
	}
}
