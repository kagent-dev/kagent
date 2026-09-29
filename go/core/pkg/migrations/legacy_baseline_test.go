package migrations

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// releasedSource replays the migrations a 1.x release created its databases
// with, recorded under the same tracking table and version as the tree's own.
func releasedSource(t *testing.T, dir string) Source {
	t.Helper()
	return Source{
		Name:          "core",
		TrackingTable: coreTrackingTable,
		FS:            os.DirFS("testdata"),
		Dir:           dir,
	}
}

func TestRunUpRefusesA1xDatabase(t *testing.T) {
	dsn := startTestDB(t)
	require.NoError(t, RunUp(t.Context(), dsn, []Source{releasedSource(t, "core-1.2")}))
	require.True(t, testTableExists(t, dsn, "agent_instance"))
	released := testVersions(t, dsn, coreTrackingTable)
	require.Contains(t, released, int64(1))

	err := RunUp(t.Context(), dsn, BuiltinSourcesInSchema(false, "public", "public"))
	require.ErrorIs(t, err, ErrLegacyBaseline)
	require.EqualError(t, err, `core precheck: the database holds the kagent 1.x schema (agent_instance tables); this line does not migrate it: point the controller at a fresh database and keep the old one for retention (see FORK.md, "Cut-over from the 1.x line")`)
	require.False(t, testTableExists(t, dsn, "session"), "the refusal must leave the 1.x database untouched")
	require.Equal(t, released, testVersions(t, dsn, coreTrackingTable))

	require.ErrorIs(t, VerifyMigrated(t.Context(), dsn, BuiltinSourcesInSchema(false, "public", "public")), ErrLegacyBaseline)
}

func TestRunUpMigratesAFreshDatabaseAndStartsOnACurrentOne(t *testing.T) {
	dsn := startTestDB(t)
	require.NoError(t, RunUp(t.Context(), dsn, BuiltinSourcesInSchema(false, "public", "public")))
	require.True(t, testTableExists(t, dsn, "session"))
	require.False(t, testTableExists(t, dsn, "agent_instance"))

	require.NoError(t, RunUp(t.Context(), dsn, BuiltinSourcesInSchema(false, "public", "public")))
	require.NoError(t, VerifyMigrated(t.Context(), dsn, BuiltinSourcesInSchema(false, "public", "public")))
}
