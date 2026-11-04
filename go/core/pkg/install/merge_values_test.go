package install

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeValuesUsesHelmPrecedence(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("Helm is required for values integration")
	}
	dir := t.TempDir()
	chart := filepath.Join(dir, "merge")
	require.NoError(t, writeValuesChart(chart))
	first, last := filepath.Join(dir, "first.yaml"), filepath.Join(dir, "last.yaml")
	require.NoError(t, os.WriteFile(first, []byte("nested: {keep: first, value: first}\nitems: [first, second]\nnullable: null\n"), 0o600))
	require.NoError(t, os.WriteFile(last, []byte("nested: {value: last}\nboolean: false\n"), 0o600))
	values, err := mergeValues(t.Context(), CommandRunner{}, Chart{
		inlineValues: "nested: {keep: inline, value: inline}\nnullable: default\nboolean: true\n",
		ValuesFiles:  []string{first, last},
		Values:       []string{"nested.value=set", "items[0]=set", `literal=a\,b`, "number=123", "stringNumber=001"},
	}, chart)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"nested": map[string]any{"keep": "first", "value": "set"},
		"items":  []any{"set", "second"}, "nullable": nil, "boolean": false,
		"literal": "a,b", "number": float64(123), "stringNumber": "001",
	}, values)
}

func TestMergeValuesDoesNotExposeDiagnostics(t *testing.T) {
	cause := errors.New("parse failed: secret-value-must-not-appear")
	runner := &recordingRunner{run: func(Command) (string, error) { return "secret-value-must-not-appear", cause }}
	_, err := mergeValues(t.Context(), runner, Chart{}, "unused")
	require.ErrorIs(t, err, cause)
	require.NotContains(t, err.Error(), "secret-value-must-not-appear")
	runner.run = func(Command) (string, error) { return "secret-value-must-not-appear", nil }
	_, err = mergeValues(t.Context(), runner, Chart{}, "unused")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-value-must-not-appear")
}
