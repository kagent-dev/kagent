package operations

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// Prepare the store's actual statements against its independently migrated
// schema, including queries on paths not reached by a particular scenario.
func TestInlineSQLPrepares(t *testing.T) {
	store := openTestStore(t, database(t), "prepare")
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, "store.go", nil, 0)
	require.NoError(t, err)
	checked := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (fn.Sel.Name != "ExecContext" && fn.Sel.Name != "QueryContext" && fn.Sel.Name != "QueryRowContext") {
			return true
		}
		query := inlineSQL(t, call.Args[1])
		t.Run(fmt.Sprintf("line_%d", positions.Position(call.Pos()).Line), func(t *testing.T) {
			statement, err := store.db.PrepareContext(t.Context(), query)
			require.NoError(t, err)
			require.NoError(t, statement.Close())
		})
		checked++
		return true
	})
	require.Positive(t, checked)
}

func inlineSQL(t *testing.T, expression ast.Expr) string {
	t.Helper()
	switch value := expression.(type) {
	case *ast.BasicLit:
		query, err := strconv.Unquote(value.Value)
		require.NoError(t, err)
		return query
	case *ast.Ident:
		require.Equal(t, "columns", value.Name, "only the store's projection constant may be interpolated")
		return columns
	case *ast.BinaryExpr:
		require.Equal(t, token.ADD, value.Op)
		return inlineSQL(t, value.X) + inlineSQL(t, value.Y)
	default:
		t.Fatalf("unsupported SQL expression %T: keep statements statically checkable", expression)
		return ""
	}
}
