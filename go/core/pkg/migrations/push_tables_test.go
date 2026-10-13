package migrations

import "testing"

// A database that applied the baseline before the push notification tables
// joined it gets them from the catch-up migration.
func TestRunUpAddsThePushTablesToAnEarlierBaseline(t *testing.T) {
	dsn := startTestDB(t)
	sources := BuiltinSourcesInSchema(false, "kagent", "extensions")
	if err := RunUp(t.Context(), dsn, sources); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	// What the earlier baseline left: version 4, no push tables, no push key.
	execSQL(t, dsn, `DROP TABLE kagent.session_push_outbox; DROP TABLE kagent.session_push_registration;
		DROP INDEX kagent.session_task_event_push_source; DELETE FROM kagent.schema_migrations WHERE version_id >= 5`)

	if err := RunUp(t.Context(), dsn, sources); err != nil {
		t.Fatalf("RunUp on the earlier baseline: %v", err)
	}
	for _, relation := range []string{"session_push_registration", "session_push_outbox", "session_task_event_push_source", "session_push_outbox_due"} {
		if !testTableExists(t, dsn, "kagent."+relation) {
			t.Errorf("kagent.%s is missing after the catch-up migration", relation)
		}
	}
	if err := VerifyMigrated(t.Context(), dsn, sources); err != nil {
		t.Fatalf("VerifyMigrated: %v", err)
	}
}
