package db

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
)

func TestMigrationSourcesFromEnvironment(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  []string
	}{
		{input: "false", want: []string{"core"}},
		{input: " TrUe ", want: []string{"core", "vector"}},
		{input: "invalid", want: []string{"core", "vector"}},
	} {
		t.Run(tt.input, func(t *testing.T) {
			t.Setenv(env.DatabaseVectorEnabled.Name(), tt.input)
			namespace := "unused"
			sources, err := migrationSources(&namespace)(t.Context())
			require.NoError(t, err)
			var names []string
			for _, source := range sources {
				names = append(names, source.Name)
			}
			require.Equal(t, tt.want, names)
		})
	}
}
