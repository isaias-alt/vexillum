package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/cli"
)

// dispatchedCommands returns the subcommand names run()'s switch routes,
// read from main.go itself so the test can't drift from the real switch.
// Flags (-h, --version, ...) are not commands and are skipped.
func dispatchedCommands(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "run" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				lit, ok := e.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", lit.Value, err)
				}
				if !strings.HasPrefix(name, "-") {
					names = append(names, name)
				}
			}
			return true
		})
	}
	sort.Strings(names)
	return names
}

func TestRegistryMatchesDispatchSwitch(t *testing.T) {
	dispatched := dispatchedCommands(t)
	if len(dispatched) == 0 {
		t.Fatal("found no dispatched commands in main.go; has run() changed shape?")
	}

	registered := map[string]bool{}
	for _, c := range cli.Commands() {
		registered[c.Name] = true
	}
	switched := map[string]bool{}
	for _, name := range dispatched {
		switched[name] = true
		if !registered[name] {
			t.Errorf("main.go dispatches %q but it has no entry in the cli command registry", name)
		}
	}
	for name := range registered {
		if !switched[name] {
			t.Errorf("registry has %q but main.go's switch never dispatches it", name)
		}
	}
}
