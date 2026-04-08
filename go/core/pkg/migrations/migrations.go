// Package migrations embeds the SQL migration files for the kagent database schema
// and provides the runner that applies them at startup.
package migrations

import "embed"

//go:embed core vector
var FS embed.FS
