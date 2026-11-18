package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPatternsGoAndYAMLAgree runs every case against the ADK Go version and
// the declarative YAML version and requires the same trace from both, plus
// the documented result.
func TestPatternsGoAndYAMLAgree(t *testing.T) {
	for _, p := range Catalog() {
		t.Run(p.Name, func(t *testing.T) {
			require.NotEmpty(t, p.Cases, "a pattern without a case proves nothing")
			for _, c := range p.Cases {
				t.Run(c.Input+fmtReplies(c.Replies), func(t *testing.T) {
					goAgent, err := p.Go()
					require.NoError(t, err)
					goTrace, err := Run(t.Context(), goAgent, c.Input, c.Replies)
					require.NoError(t, err)

					yamlAgent, err := p.BuildYAML(t.Context())
					require.NoError(t, err)
					yamlTrace, err := Run(t.Context(), yamlAgent, c.Input, c.Replies)
					require.NoError(t, err)

					require.Equal(t, p.Normalize(goTrace), p.Normalize(yamlTrace), "Go and YAML versions diverged")
					for _, want := range c.Want {
						require.Contains(t, goTrace, want)
					}
					for _, absent := range c.Absent {
						require.NotContains(t, goTrace, absent)
					}
				})
			}
		})
	}
}

func fmtReplies(replies []string) string {
	if len(replies) == 0 {
		return ""
	}
	return " reply=" + replies[0]
}

func TestCatalogIsConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Catalog() {
		require.False(t, seen[p.Name], "duplicate pattern %s", p.Name)
		seen[p.Name] = true
		_, err := p.YAML()
		require.NoError(t, err, "pattern %s has no patterns/%s.yaml", p.Name, p.Name)
		require.NotEmpty(t, p.GoFuncName())
		require.NotEmpty(t, p.Title)
		require.NotEmpty(t, p.Summary)
	}
	entries, err := patternFiles.ReadDir("patterns")
	require.NoError(t, err)
	require.Len(t, entries, len(seen), "every YAML file belongs to a catalog entry")

	antiSeen := map[string]bool{}
	for _, a := range AntiPatterns() {
		require.False(t, antiSeen[a.Name], "duplicate anti-pattern %s", a.Name)
		antiSeen[a.Name] = true
		_, err := a.YAML()
		require.NoError(t, err, "anti-pattern %s has no anti/%s.yaml", a.Name, a.Name)
		require.NotEmpty(t, a.WantErr)
	}
	entries, err = patternFiles.ReadDir("anti")
	require.NoError(t, err)
	require.Len(t, entries, len(antiSeen), "every anti-pattern YAML file belongs to a catalog entry")
}

// TestAntiPatternsFailTheSameWay checks that the engine rejects each bad graph
// whether it is built in Go or declared in YAML, and that the message names
// the problem.
func TestAntiPatternsFailTheSameWay(t *testing.T) {
	for _, a := range AntiPatterns() {
		t.Run(a.Name, func(t *testing.T) {
			if !a.AtRun {
				if a.Go != nil {
					_, err := a.Go()
					require.ErrorContains(t, err, a.WantErr, "ADK Go version")
				}
				_, err := a.BuildYAML(t.Context())
				require.ErrorContains(t, err, a.WantErr, "YAML version")
				return
			}

			// Accepted when built, rejected when run.
			goAgent, err := a.Go()
			require.NoError(t, err)
			_, err = Run(t.Context(), goAgent, a.Input, nil)
			require.ErrorContains(t, err, a.WantErr, "ADK Go version")

			yamlAgent, err := a.BuildYAML(t.Context())
			require.NoError(t, err)
			_, err = Run(t.Context(), yamlAgent, a.Input, nil)
			require.ErrorContains(t, err, a.WantErr, "YAML version")
		})
	}
}
