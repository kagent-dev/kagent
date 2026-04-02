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

// contractingPatterns lists DDL that shrinks or renames the schema.
// These are forbidden in up migrations — schema changes must be additive-only.
// Down migrations are intentionally excluded from this check.
var contractingPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	// Removing a table entirely.
	{"DROP TABLE", regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`)},
	// Removing a column with the explicit COLUMN keyword.
	{"DROP COLUMN", regexp.MustCompile(`(?i)\bDROP\s+COLUMN\b`)},
	// Renaming a table changes the name old code references.
	{"RENAME TABLE", regexp.MustCompile(`(?i)\bALTER\s+TABLE\b[^;]+\bRENAME\s+TO\b`)},
	// Renaming a column breaks any code or query that still uses the old name.
	{"RENAME COLUMN", regexp.MustCompile(`(?i)\bRENAME\s+COLUMN\b`)},
}

// alterDropRe captures the word immediately after ALTER TABLE ... DROP [IF EXISTS].
// The COLUMN keyword is optional in Postgres, so "ALTER TABLE foo DROP bar" is
// a valid column removal. We capture the first word after DROP and check whether
// it is a known non-contracting variant (COLUMN, CONSTRAINT, DEFAULT, NOT).
var alterDropRe = regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+\S+\s+DROP\s+(?:IF\s+EXISTS\s+)?(\w+)`)

// safeDropKeywords are words that can legitimately follow DROP in an ALTER TABLE
// without removing a column.
var safeDropKeywords = map[string]bool{
	"column":     true, // already caught by the DROP COLUMN pattern above
	"constraint": true, // removes a constraint, not a column
	"default":    true, // removes a column default, not the column itself
	"not":        true, // ALTER TABLE t ALTER COLUMN c DROP NOT NULL
}

// TestNoContractingDDL enforces the additive-only schema policy: up migrations
// must never remove or rename tables or columns. Down migrations are excluded
// because they exist specifically to reverse schema changes.
func TestNoContractingDDL(t *testing.T) {
	tracks := []string{"core", "vector"}

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
			content := string(data)

			for _, p := range contractingPatterns {
				if m := p.re.FindString(content); m != "" {
					t.Errorf(
						"contracting DDL in %s/%s: %q matches %q — up migrations must be additive-only",
						track, path, m, p.name,
					)
				}
			}

			// Check for bare column drops: ALTER TABLE foo DROP bar (no COLUMN keyword).
			// RE2 has no negative lookahead, so we capture the word and filter here.
			for _, m := range alterDropRe.FindAllStringSubmatch(content, -1) {
				if !safeDropKeywords[strings.ToLower(m[1])] {
					t.Errorf(
						"contracting DDL in %s/%s: %q — bare DROP without COLUMN keyword is a column removal; up migrations must be additive-only",
						track, path, m[0],
					)
				}
			}

			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir(%q): %v", track, err)
		}
	}
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
