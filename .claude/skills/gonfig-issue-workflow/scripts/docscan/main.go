// Command docscan lists the functions touched by a diff and whether they have a doc comment.
//
// CodeRabbit's docstring coverage check counts the functions touched by a pull request, including tests,
// so this predicts its result before pushing:
//
//	git diff -U0 main | go run .claude/skills/gonfig-issue-workflow/scripts/docscan
//
// Untracked files are not part of git diff, so add them with `git add -N` first.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// main reads a diff from stdin and prints each touched function with its doc comment status
func main() {
	changed := map[string]map[int]bool{}
	hunk := regexp.MustCompile(`^@@ -\S+ \+(\d+)(?:,(\d+))? @@`)

	file := ""
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if name, found := strings.CutPrefix(line, "+++ b/"); found {
			file = name
			continue
		}
		if m := hunk.FindStringSubmatch(line); m != nil && file != "" {
			start, _ := strconv.Atoi(m[1])
			n := 1
			if m[2] != "" {
				n, _ = strconv.Atoi(m[2])
			}
			if changed[file] == nil {
				changed[file] = map[int]bool{}
			}
			for l := start; l < start+n; l++ {
				changed[file][l] = true
			}
		}
	}

	var results []string
	missing, total := 0, 0
	for name, lines := range changed {
		if !strings.HasSuffix(name, ".go") {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			continue
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			from, to := fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line
			for l := from; l <= to; l++ {
				if lines[l] {
					total++
					status := "documented"
					if fn.Doc == nil {
						status = "MISSING"
						missing++
					}
					results = append(results, fmt.Sprintf("%-10s %s:%d %s", status, name, from, fn.Name.Name))
					break
				}
			}
		}
	}

	sort.Strings(results)
	for _, result := range results {
		fmt.Println(result)
	}
	fmt.Printf("%d of %d touched functions without doc comment\n", missing, total)
}
