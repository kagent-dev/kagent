// Package migrations exports the embedded SQL migration files for the kagent OSS
// database schema. Enterprise builds import this FS to bundle OSS migrations
// alongside enterprise-specific ones at build time.
package migrations

import "embed"

//go:embed core vector
var FS embed.FS
