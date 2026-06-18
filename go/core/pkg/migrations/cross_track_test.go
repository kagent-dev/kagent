package migrations_test

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

// Cross-track DDL rules:
//
//   - Each track owns the tables it creates (via CREATE TABLE).
//   - A track must not ALTER TABLE or CREATE INDEX ON a table owned by another track.
//
// This is a static analysis check against the embedded migration files. It runs
// against real SQL — no database required.

var (
	createTableRe = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	alterTableRe  = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(\w+)`)
	createIndexRe = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\s+)?ON\s+(\w+)`)
)

// ownedTables returns the set of table names created by up migrations in fsys.
func ownedTables(fsys fs.FS) (map[string]string, error) {
	tables := make(map[string]string) // table name → file that created it
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".up.sql") {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		for _, m := range createTableRe.FindAllSubmatch(data, -1) {
			name := strings.ToLower(string(m[1]))
			tables[name] = path
		}
		return nil
	})
	return tables, err
}

type violation struct {
	file      string
	statement string
	table     string
	ownedBy   string
}

// crossTrackViolations returns any up-migration DDL in fsys that modifies a
// table owned by another track.
func crossTrackViolations(fsys fs.FS, foreignTables map[string]string) ([]violation, error) {
	var violations []violation
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".up.sql") {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		content := string(data)

		check := func(matches [][]string) {
			for _, m := range matches {
				table := strings.ToLower(m[1])
				if owner, ok := foreignTables[table]; ok {
					violations = append(violations, violation{
						file:      path,
						statement: m[0],
						table:     table,
						ownedBy:   owner,
					})
				}
			}
		}
		check(alterTableRe.FindAllStringSubmatch(content, -1))
		check(createIndexRe.FindAllStringSubmatch(content, -1))
		return nil
	})
	return violations, err
}

// guardCheck describes a DDL statement that requires an idempotency guard.
// re captures the first significant word after the keyword; if that word is not
// "if" (case-insensitive) the guard is absent.
type guardCheck struct {
	name string
	re   *regexp.Regexp
}

// upGuardChecks are statements in up migrations that must use IF NOT EXISTS.
var upGuardChecks = []guardCheck{
	{"CREATE TABLE", regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+(\w+)`)},
	{"CREATE INDEX", regexp.MustCompile(`(?i)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(\w+)`)},
	{"CREATE EXTENSION", regexp.MustCompile(`(?i)\bCREATE\s+EXTENSION\s+(\w+)`)},
	{"ADD COLUMN", regexp.MustCompile(`(?i)\bADD\s+COLUMN\s+(\w+)`)},
}

// downGuardChecks are statements in down migrations that must use IF EXISTS.
var downGuardChecks = []guardCheck{
	{"DROP TABLE", regexp.MustCompile(`(?i)\bDROP\s+TABLE\s+(\w+)`)},
	{"DROP INDEX", regexp.MustCompile(`(?i)\bDROP\s+INDEX\s+(\w+)`)},
	{"DROP EXTENSION", regexp.MustCompile(`(?i)\bDROP\s+EXTENSION\s+(\w+)`)},
	{"DROP COLUMN", regexp.MustCompile(`(?i)\bDROP\s+COLUMN\s+(\w+)`)},
}

// TestMigrationGuards enforces idempotency guards across all migration files:
//   - Up migrations: CREATE TABLE/INDEX/EXTENSION and ADD COLUMN must use IF NOT EXISTS.
//   - Down migrations: DROP TABLE/INDEX/EXTENSION/COLUMN must use IF EXISTS.
//
// This ensures migrations are safe to re-run and that the two-track rollback
// logic can call down migrations more than once without errors.
func TestMigrationGuards(t *testing.T) {
	tracks := []string{"core", "vector"}

	for _, track := range tracks {
		sub, err := fs.Sub(migrations.FS, track)
		if err != nil {
			t.Fatalf("fs.Sub(%q): %v", track, err)
		}

		err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}

			var checks []guardCheck
			switch {
			case strings.HasSuffix(path, ".up.sql"):
				checks = upGuardChecks
			case strings.HasSuffix(path, ".down.sql"):
				checks = downGuardChecks
			default:
				return nil
			}

			data, err := fs.ReadFile(sub, path)
			if err != nil {
				return err
			}
			content := string(data)

			for _, c := range checks {
				for _, m := range c.re.FindAllStringSubmatch(content, -1) {
					if !strings.EqualFold(m[1], "if") {
						t.Errorf(
							"missing guard in %s/%s: %q — %s requires IF NOT EXISTS / IF EXISTS",
							track, path, m[0], c.name,
						)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir(%q): %v", track, err)
		}
	}
}

// TestNoCrossTrackDDL verifies that no migration track modifies tables owned
// by another track. Each track must only ALTER or index its own tables.
func TestNoCrossTrackDDL(t *testing.T) {
	tracks := []string{"core", "vector"}

	// Build the ownership map for each track.
	owned := make(map[string]map[string]string, len(tracks))
	for _, track := range tracks {
		sub, err := fs.Sub(migrations.FS, track)
		if err != nil {
			t.Fatalf("fs.Sub(%q): %v", track, err)
		}
		tables, err := ownedTables(sub)
		if err != nil {
			t.Fatalf("ownedTables(%q): %v", track, err)
		}
		owned[track] = tables
	}

	// For each track, check its migrations don't touch tables owned by others.
	for _, track := range tracks {
		sub, err := fs.Sub(migrations.FS, track)
		if err != nil {
			t.Fatalf("fs.Sub(%q): %v", track, err)
		}

		// Collect all tables owned by *other* tracks.
		foreign := make(map[string]string)
		for otherTrack, tables := range owned {
			if otherTrack == track {
				continue
			}
			for table, file := range tables {
				foreign[table] = fmt.Sprintf("%s/%s", otherTrack, file)
			}
		}

		violations, err := crossTrackViolations(sub, foreign)
		if err != nil {
			t.Fatalf("crossTrackViolations(%q): %v", track, err)
		}
		for _, v := range violations {
			t.Errorf(
				"cross-track DDL violation: %s/%s contains %q targeting table %q (owned by %s)",
				track, v.file, v.statement, v.table, v.ownedBy,
			)
		}
	}
}

// --- Shared SQL helpers ---

// stripSQLComments removes `--` line comments so the static checks below match
// real statements, not commented-out or explanatory SQL.
func stripSQLComments(s string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// splitStatements splits SQL into individual statements on `;`, dropping blanks.
func splitStatements(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// --- Schema-agnostic SQL ---
//
// Migration SQL must not name a schema: the schema a migration lands in is
// chosen by the connection (search_path), not the file, so the same files apply
// into whatever schema the connection selects. See database-migrations.md,
// "Schema-agnostic SQL". Static check over every migration file — no database.

var schemaQualifiedChecks = []guardCheck{
	{"CREATE SCHEMA", regexp.MustCompile(`(?i)\bCREATE\s+SCHEMA\b`)},
	{"DROP SCHEMA", regexp.MustCompile(`(?i)\bDROP\s+SCHEMA\b`)},
	{"search_path", regexp.MustCompile(`(?i)\bsearch_path\b`)},
	{"SET SCHEMA", regexp.MustCompile(`(?i)\bSET\s+SCHEMA\b`)},
	{"schema-qualified table", regexp.MustCompile(`(?i)\b(?:CREATE|ALTER|DROP)\s+TABLE\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?\w+\.\w+`)},
	{"schema-qualified index target", regexp.MustCompile(`(?i)\bON\s+\w+\.\w+`)},
	{"schema-qualified reference", regexp.MustCompile(`(?i)\bREFERENCES\s+\w+\.\w+`)},
}

// schemaViolations returns the names of schema-qualified patterns found in SQL.
func schemaViolations(sql string) []string {
	content := stripSQLComments(sql)
	var found []string
	for _, c := range schemaQualifiedChecks {
		if c.re.MatchString(content) {
			found = append(found, c.name)
		}
	}
	return found
}

// TestSchemaAgnosticSQL rejects any schema name in migration SQL. The connection
// selects the schema; a hard-coded one breaks any deployment that runs the track
// in a different schema.
func TestSchemaAgnosticSQL(t *testing.T) {
	tracks := []string{"core", "vector"}
	for _, track := range tracks {
		sub, err := fs.Sub(migrations.FS, track)
		if err != nil {
			t.Fatalf("fs.Sub(%q): %v", track, err)
		}
		err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".sql") {
				return err
			}
			data, err := fs.ReadFile(sub, path)
			if err != nil {
				return err
			}
			for _, v := range schemaViolations(string(data)) {
				t.Errorf(
					"schema reference in %s/%s: %s; migrations must be schema-agnostic (the connection selects the schema)",
					track, path, v,
				)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir(%q): %v", track, err)
		}
	}
}

// --- Contraction guard (backward compatibility) ---
//
// A "contraction" is up-migration DDL that breaks what a prior release's code
// reads or writes: dropping or renaming shipped objects, narrowing a type, or
// adding a constraint an old writer would violate. Contractions are blocked by
// default; an intentional one must be declared in declaredContractions in the
// same PR so the change is visible for review. See database-migrations.md,
// "Backward compatibility and contraction".
//
// DDL targeting a table or column created in the *same* migration file is new
// structure, not a contraction, and is exempt. Data rewrites (UPDATEs that
// change stored format) are also contractions but are not statically
// detectable, so they are out of scope for this check.

// declaredContractions allowlists up-migration files that intentionally contain
// contraction-class DDL, mapping each to why it is allowed. To land a new
// contraction, add an entry here in the same PR — naming the release the
// replacement shipped in — so the reviewer can confirm the replacement shipped
// at or before the previous minor and that no current code reads the removed
// structure.
var declaredContractions = map[string]string{
	"core/000002_not_null_defaults.up.sql":  "pre-rule (grandfathered): backfills NULLs, then SET NOT NULL on columns from 000001 that always carried a DEFAULT",
	"core/000004_feedback_single_pk.up.sql": "pre-rule (grandfathered): normalizes the feedback primary key, converging GORM-era composite PKs",
}

var (
	dropTableNameRe   = regexp.MustCompile(`(?i)\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(\w+)`)
	addColumnNameRe   = regexp.MustCompile(`(?i)\bADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)`)
	alterColumnNameRe = regexp.MustCompile(`(?i)\bALTER\s+COLUMN\s+(\w+)`)
	dropColumnNameRe  = regexp.MustCompile(`(?i)\bDROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?(\w+)`)
	notNullRe         = regexp.MustCompile(`(?i)\bNOT\s+NULL\b`)
	defaultRe         = regexp.MustCompile(`(?i)\bDEFAULT\b`)
)

// contractionChecks classify a single, comment-stripped statement.
var contractionChecks = []guardCheck{
	{"DROP TABLE", regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`)},
	{"DROP COLUMN", regexp.MustCompile(`(?i)\bDROP\s+COLUMN\b`)},
	{"DROP CONSTRAINT", regexp.MustCompile(`(?i)\bDROP\s+CONSTRAINT\b`)},
	{"RENAME", regexp.MustCompile(`(?i)\bRENAME\s+(?:TO|COLUMN|CONSTRAINT)\b`)},
	{"ALTER COLUMN TYPE", regexp.MustCompile(`(?i)\bALTER\s+COLUMN\s+\w+\s+(?:SET\s+DATA\s+)?TYPE\b`)},
	{"SET NOT NULL", regexp.MustCompile(`(?i)\bSET\s+NOT\s+NULL\b`)},
	{"ADD CONSTRAINT", regexp.MustCompile(`(?i)\bADD\s+(?:CONSTRAINT|PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE|CHECK)\b`)},
}

// stmtTable returns the ALTER/DROP TABLE target in a statement, lowercased.
func stmtTable(stmt string) string {
	if m := alterTableRe.FindStringSubmatch(stmt); m != nil {
		return strings.ToLower(m[1])
	}
	if m := dropTableNameRe.FindStringSubmatch(stmt); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// stmtColumn returns the ALTER/DROP COLUMN target in a statement, lowercased.
func stmtColumn(stmt string) string {
	if m := alterColumnNameRe.FindStringSubmatch(stmt); m != nil {
		return strings.ToLower(m[1])
	}
	if m := dropColumnNameRe.FindStringSubmatch(stmt); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// fileContractions returns the contraction-class statements in an up-migration's
// SQL. DDL targeting a table or column created in the same file is exempt as new
// structure. The returned names are deduplicated.
func fileContractions(sql string) []string {
	content := stripSQLComments(sql)

	// New structure created in this same file is exempt.
	localTables := make(map[string]bool)
	for _, m := range createTableRe.FindAllStringSubmatch(content, -1) {
		localTables[strings.ToLower(m[1])] = true
	}
	localColumns := make(map[string]bool)
	for _, m := range addColumnNameRe.FindAllStringSubmatch(content, -1) {
		localColumns[strings.ToLower(m[1])] = true
	}

	var found []string
	for _, stmt := range splitStatements(content) {
		if tbl := stmtTable(stmt); tbl != "" && localTables[tbl] {
			continue // operates on a table created in this file
		}
		for _, c := range contractionChecks {
			if !c.re.MatchString(stmt) {
				continue
			}
			if col := stmtColumn(stmt); col != "" && localColumns[col] {
				continue // operates on a column added in this file
			}
			found = append(found, c.name)
		}
		// A new NOT NULL column without a default breaks old INSERTs.
		if addColumnNameRe.MatchString(stmt) && notNullRe.MatchString(stmt) && !defaultRe.MatchString(stmt) {
			found = append(found, "ADD COLUMN ... NOT NULL without DEFAULT")
		}
	}
	return uniqueStrings(found)
}

// TestNoUndeclaredContractions blocks contraction-class DDL in up migrations
// unless the file is listed in declaredContractions.
func TestNoUndeclaredContractions(t *testing.T) {
	tracks := []string{"core", "vector"}
	matched := make(map[string]bool) // allowlist entries that actually fired

	for _, track := range tracks {
		sub, err := fs.Sub(migrations.FS, track)
		if err != nil {
			t.Fatalf("fs.Sub(%q): %v", track, err)
		}
		err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".up.sql") {
				return err
			}
			data, err := fs.ReadFile(sub, path)
			if err != nil {
				return err
			}
			full := track + "/" + path
			found := fileContractions(string(data))
			if len(found) == 0 {
				return nil
			}
			if _, ok := declaredContractions[full]; ok {
				matched[full] = true
				return nil
			}
			t.Errorf(
				"undeclared contraction in %s: %s\n"+
					"  This can break the previous release's code during a rollback.\n"+
					"  If intentional: split expand/contract across minors, then add an entry to\n"+
					"  declaredContractions in cross_track_test.go for reviewer sign-off.\n"+
					"  See database-migrations.md, \"Backward compatibility and contraction\".",
				full, strings.Join(found, ", "),
			)
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir(%q): %v", track, err)
		}
	}

	for key := range declaredContractions {
		if !matched[key] {
			t.Errorf("stale declaredContractions entry %q: no contraction detected (file changed or removed?) — remove the entry", key)
		}
	}
}

// --- Unit tests for the detectors ---

func TestFileContractions(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{"add nullable column", `ALTER TABLE agent ADD COLUMN IF NOT EXISTS note TEXT;`, nil},
		{"add column with default", `ALTER TABLE agent ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'x';`, nil},
		{"create table then add constraint same file", "CREATE TABLE IF NOT EXISTS t (id TEXT);\nALTER TABLE t ADD CONSTRAINT t_pk PRIMARY KEY (id);", nil},
		{"add column then set not null same file", "ALTER TABLE agent ADD COLUMN IF NOT EXISTS w TEXT;\nALTER TABLE agent ALTER COLUMN w SET DEFAULT 'd';\nALTER TABLE agent ALTER COLUMN w SET NOT NULL;", nil},
		{"contraction only in comment", "-- DROP COLUMN foo from agent\nALTER TABLE agent ADD COLUMN IF NOT EXISTS bar TEXT;", nil},
		{"drop column on shipped table", `ALTER TABLE agent DROP COLUMN IF EXISTS config;`, []string{"DROP COLUMN"}},
		{"set not null on shipped column", `ALTER TABLE feedback ALTER COLUMN is_positive SET NOT NULL;`, []string{"SET NOT NULL"}},
		{"add constraint on shipped table", `ALTER TABLE feedback ADD CONSTRAINT fk FOREIGN KEY (user_id) REFERENCES agent(id);`, []string{"ADD CONSTRAINT"}},
		{"rename column", `ALTER TABLE agent RENAME COLUMN config TO cfg;`, []string{"RENAME"}},
		{"alter column type", `ALTER TABLE agent ALTER COLUMN config TYPE JSONB;`, []string{"ALTER COLUMN TYPE"}},
		{"drop table", `DROP TABLE IF EXISTS agent;`, []string{"DROP TABLE"}},
		{"add not null column without default", `ALTER TABLE agent ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL;`, []string{"ADD COLUMN ... NOT NULL without DEFAULT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileContractions(tt.sql)
			if !equalStringSets(got, tt.want) {
				t.Errorf("fileContractions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSchemaViolations(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{"unqualified table", `CREATE TABLE IF NOT EXISTS eval_set (id TEXT);`, nil},
		{"unqualified index", `CREATE INDEX IF NOT EXISTS i ON eval_set(id);`, nil},
		{"schema in comment", `-- create table myschema.foo here` + "\n" + `CREATE TABLE IF NOT EXISTS foo (id TEXT);`, nil},
		{"qualified table", `CREATE TABLE myschema.eval_set (id TEXT);`, []string{"schema-qualified table"}},
		{"create schema", `CREATE SCHEMA IF NOT EXISTS myschema;`, []string{"CREATE SCHEMA"}},
		{"set search_path", `SET search_path TO myschema;`, []string{"search_path"}},
		{"set schema", `ALTER TABLE foo SET SCHEMA myschema;`, []string{"SET SCHEMA"}},
		{"qualified index target", `CREATE INDEX i ON myschema.foo(id);`, []string{"schema-qualified index target"}},
		{"qualified reference", `ALTER TABLE foo ADD CONSTRAINT fk FOREIGN KEY (b) REFERENCES myschema.bar(id);`, []string{"schema-qualified reference"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schemaViolations(tt.sql)
			if !equalStringSets(got, tt.want) {
				t.Errorf("schemaViolations() = %v, want %v", got, tt.want)
			}
		})
	}
}

// equalStringSets compares two string slices ignoring order and duplicates.
func equalStringSets(a, b []string) bool {
	sa, sb := make(map[string]bool), make(map[string]bool)
	for _, s := range a {
		sa[s] = true
	}
	for _, s := range b {
		sb[s] = true
	}
	if len(sa) != len(sb) {
		return false
	}
	for s := range sa {
		if !sb[s] {
			return false
		}
	}
	return true
}
