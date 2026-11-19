// Run from the repository root: go run ./scripts/qual-route-inventory.go [-write]
// This reads source only. It never starts the server or sends requests.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
)

const matrix = "docs/quality/route-invariant-matrix.txt"
const start = "BEGIN SOURCE INVENTORY\n"

func expression(expr ast.Expr) string {
	var b bytes.Buffer
	if err := format.Node(&b, token.NewFileSet(), expr); err != nil {
		log.Fatal(err)
	}
	return b.String()
}

func literal(expr ast.Expr) string {
	value, ok := expr.(*ast.BasicLit)
	if !ok || value.Kind != token.STRING {
		log.Fatalf("expected literal registration ID, got %s", expression(expr))
	}
	s, err := strconv.Unquote(value.Value)
	if err != nil || s == "" {
		log.Fatalf("invalid registration ID: %s", value.Value)
	}
	return s
}

func wrapper(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "none at registration"
	}
	switch fn := call.Fun.(type) {
	case *ast.CallExpr:
		return expression(fn)
	case *ast.Ident:
		switch fn.Name {
		case "AuthMiddleware", "OptionalAuthMiddleware":
			return fn.Name
		}
	}
	return "none at registration"
}

func scan(path string, visit func(ast.Node) bool) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		log.Fatal(err)
	}
	ast.Inspect(f, visit)
}

func routes(path, listener string) []string {
	var rows []string
	scan(path, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
			return true
		}
		if len(call.Args) != 2 {
			log.Fatalf("unexpected registration in %s: %s", path, expression(call))
		}
		rows = append(rows, fmt.Sprintf("%s | %s | %s", listener, literal(call.Args[0]), wrapper(call.Args[1])))
		return true
	})
	if len(rows) == 0 {
		log.Fatal("no routes found in ", path)
	}
	return rows
}

func jobs() []string {
	const path = "backend/internal/worker/operations.go"
	var rows []string
	scan(path, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "jobs" {
			return true
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			entry, ok := n.(*ast.CompositeLit)
			if !ok || entry.Type != nil {
				return true
			}
			fields := map[string]ast.Expr{}
			for _, elt := range entry.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if name, ok := kv.Key.(*ast.Ident); ok {
						fields[name.Name] = kv.Value
					}
				}
			}
			if fields["ID"] == nil || fields["Period"] == nil || fields["Timeout"] == nil {
				log.Fatal("unexpected job registry shape in ", path)
			}
			rows = append(rows, fmt.Sprintf("worker | %s | period %s, timeout %s", literal(fields["ID"]), expression(fields["Period"]), expression(fields["Timeout"])))
			return false
		})
		return false
	})
	if len(rows) == 0 {
		log.Fatal("no jobs found in ", path)
	}
	return rows
}

func main() {
	write := flag.Bool("write", false, "replace generated inventory, preserving hand-reviewed notes")
	flag.Parse()
	log.SetFlags(0)
	rows := append(routes("backend/cmd/server/main.go", "app"), routes("backend/internal/observability/metrics.go", "metrics")...)
	routeCount := len(rows)
	rows = append(rows, jobs()...)
	sort.Strings(rows)
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		parts := strings.SplitN(row, " | ", 3)
		key := strings.Join(parts[:2], " | ")
		if seen[key] {
			log.Fatal("duplicate registration: ", key)
		}
		seen[key] = true
	}
	data, err := os.ReadFile(matrix)
	if err != nil {
		log.Fatal(err)
	}
	old := strings.ReplaceAll(string(data), "\r\n", "\n")
	prefix, _, ok := strings.Cut(old, start)
	if !ok || strings.Count(old, start) != 1 {
		log.Fatal("expected one inventory marker in ", matrix)
	}
	want := prefix + start + strings.Join(rows, "\n") + "\n"
	if *write {
		if err := os.WriteFile(matrix, []byte(want), 0644); err != nil {
			log.Fatal(err)
		}
	} else if old != want {
		log.Fatal("route inventory is stale: go run ./scripts/qual-route-inventory.go -write")
	}
	fmt.Printf("Inventory: %d HTTP registrations, %d worker jobs\n", routeCount, len(rows)-routeCount)
}
