package goq_test

import (
	"bytes"
	"go/ast"
	"go/doc"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestOperatorInventoryMatchesSpec cross-checks the hand-maintained operator
// table in the design spec (§6) against the package's real exported API, in
// both directions. A prior review found the table had silently drifted from
// the code — this makes that drift a test failure instead of something a
// human has to notice.
//
// "Operator" here means: every method on one of the six pipeline types
// (Query, TryQuery, ParQuery, OrderedQuery, GroupQuery, ChunkQuery), plus
// every package-level function whose signature mentions one of them — which
// is how the free functions that exist only because a method cannot
// constrain its receiver's type parameter (Contains, SequenceEqual, ToSet,
// Sum, Average, Min, Max, and package-level GroupBy) get included without
// naming them individually.
//
// Deliberately exempted from the reverse check (present in the API, absent
// from the table) because the spec says so explicitly: the ...Err/...Ctx
// fallible/context-aware variants, the As* transitions between pipeline
// types, and ForEach (documented in the spec as intentionally excluded from
// the table).
func TestOperatorInventoryMatchesSpec(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, parseErr := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if parseErr != nil {
			t.Fatalf("ParseFile(%q): %v", name, parseErr)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatalf("found no non-test .go files in the package directory")
	}
	// Mode 0 (not doc.AllDecls) keeps only exported symbols, matching what a
	// caller of the package can actually see.
	docPkg, err := doc.NewFromFiles(fset, files, "github.com/oleexo/goq")
	if err != nil {
		t.Fatalf("NewFromFiles: %v", err)
	}

	pipelineTypes := map[string]bool{
		"Query": true, "TryQuery": true, "ParQuery": true,
		"OrderedQuery": true, "GroupQuery": true, "ChunkQuery": true,
	}

	api := map[string]bool{}       // every exported func/method: the forward-check universe
	operators := map[string]bool{} // operator candidates: the reverse-check universe

	renderType := func(expr ast.Expr) string {
		var buf bytes.Buffer
		if fmtErr := format.Node(&buf, fset, expr); fmtErr != nil {
			return ""
		}
		return buf.String()
	}
	// mentionsPipelineType reports whether a rendered field list mentions any
	// of the six pipeline types. All six are generic and so, when mentioned,
	// always appear as "<Name>[...]" — so a single substring check on the
	// shared "Query[" suffix catches Query, TryQuery, ParQuery, OrderedQuery,
	// GroupQuery, and ChunkQuery at once.
	mentionsPipelineType := func(fields *ast.FieldList) bool {
		if fields == nil {
			return false
		}
		for _, f := range fields.List {
			if strings.Contains(renderType(f.Type), "Query[") {
				return true
			}
		}
		return false
	}
	isOperatorFunc := func(fn *doc.Func) bool {
		decl := fn.Decl.Type
		return mentionsPipelineType(decl.Params) || mentionsPipelineType(decl.Results)
	}

	for _, typ := range docPkg.Types {
		for _, m := range typ.Methods {
			api[m.Name] = true
			if pipelineTypes[typ.Name] {
				operators[m.Name] = true
			}
		}
		// go/doc attaches a top-level function to a type (as typ.Funcs, not
		// docPkg.Funcs) when the function's signature is judged to construct
		// or return that type — this is where From, FromChan, Distinct,
		// Union, and the other receiver-less operators actually live.
		for _, fn := range typ.Funcs {
			api[fn.Name] = true
			if isOperatorFunc(fn) {
				operators[fn.Name] = true
			}
		}
	}

	for _, fn := range docPkg.Funcs {
		api[fn.Name] = true
		if isOperatorFunc(fn) {
			operators[fn.Name] = true
		}
	}

	const specPath = "docs/superpowers/specs/2026-08-21-goq-design.md"
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	tableNames := parseOperatorTable(t, string(data))
	tableSet := map[string]bool{}
	for _, n := range tableNames {
		tableSet[n] = true
	}

	// Forward: every table entry must name a real exported symbol.
	for _, name := range tableNames {
		if !api[name] {
			t.Errorf("spec §6 lists %q but goq has no exported symbol of that name", name)
		}
	}

	// Reverse: every operator candidate must appear in the table, modulo the
	// spec's own stated exemptions.
	exempt := regexp.MustCompile(`^(.+Err|.+Ctx|As[A-Z].*|ForEach)$`)
	var missing []string
	for name := range operators {
		if tableSet[name] || exempt.MatchString(name) {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("operators exist in the API but are absent from spec §6's inventory table: %v", missing)
	}
}

// parseOperatorTable extracts every backtick-quoted identifier from the
// "Operators" column of the markdown table under "## 6. Operator inventory",
// stripping any generic type-parameter suffix (e.g. "Select[R]" -> "Select")
// so names compare directly against go/doc's method and function names.
func parseOperatorTable(t *testing.T, specText string) []string {
	t.Helper()
	lines := strings.Split(specText, "\n")

	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## 6. Operator inventory") {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("could not find %q in spec", "## 6. Operator inventory")
	}

	var names []string
	inTable := false
	codeRe := regexp.MustCompile("`([^`]+)`")
	for _, l := range lines[start+1:] {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "### 6.1") {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			if inTable {
				break // the table has ended
			}
			continue // still in the prose before the table
		}
		if strings.HasPrefix(trimmed, "| Family") {
			continue // header row
		}
		if strings.Contains(trimmed, "---") {
			inTable = true
			continue // separator row
		}
		cols := strings.Split(trimmed, "|")
		if len(cols) < 3 {
			continue
		}
		for _, m := range codeRe.FindAllStringSubmatch(cols[2], -1) {
			raw := m[1]
			if idx := strings.Index(raw, "["); idx >= 0 {
				raw = raw[:idx] // strip generic type parameters
			}
			names = append(names, raw)
		}
	}
	if len(names) == 0 {
		t.Fatalf("parsed zero operator names from the §6 table; parser likely out of sync with the spec's markdown")
	}
	return names
}
