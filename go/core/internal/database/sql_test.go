package database

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInlineSQLPrepares checks every inline statement against the migrated schema,
// including branches the lifecycle tests may not execute. Preparing never runs it.
func TestInlineSQLPrepares(t *testing.T) {
	db := setupTestDB(t)
	conn, err := db.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	seen := make(map[string]bool)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") || path == "sql.go" {
			continue
		}
		positions := token.NewFileSet()
		file, err := parser.ParseFile(positions, path, nil, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			index := -1
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name == "queryOne" || fn.Name == "queryMany" || fn.Name == "execSQL" {
					index = 2
				}
			case *ast.SelectorExpr:
				if fn.Sel.Name == "Exec" || fn.Sel.Name == "Query" || fn.Sel.Name == "QueryRow" {
					index = 1
				}
			}
			if index < 0 {
				return true
			}
			position := positions.Position(call.Pos())
			literal, ok := call.Args[index].(*ast.BasicLit)
			require.True(t, ok, "%s: keep SQL literal so schema validation covers it", position)
			sql, err := strconv.Unquote(literal.Value)
			require.NoError(t, err)
			if !seen[sql] {
				seen[sql] = true
				t.Run(fmt.Sprintf("%s:%d", path, position.Line), func(t *testing.T) {
					_, err := conn.Conn().Prepare(t.Context(), "", sql)
					require.NoError(t, err)
				})
			}
			return true
		})
	}
	require.NotEmpty(t, seen, "no inline SQL statements were checked")
}
