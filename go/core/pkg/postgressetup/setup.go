// Package postgressetup provides the PostgreSQL identity setup for Kagent.
package postgressetup

import _ "embed"

// Identity names that kagent install creates in its bundled database.
const (
	Schema        = "kagent"
	OwnerRole     = "kagent_owner"
	OwnerUser     = "kagent_owner_user"
	ReadWriteRole = "kagent_readwrite"
	ReadWriteUser = "kagent_readwrite_user"
)

// Config names the schema and identities created by SQL.
type Config struct {
	Schema            string
	OwnerRole         string
	OwnerUser         string
	OwnerPassword     string
	ReadWriteRole     string
	ReadWriteUser     string
	ReadWritePassword string
}

// DefaultConfig returns the passwordless identities used by bundled installs.
func DefaultConfig() Config {
	return Config{
		Schema:        Schema,
		OwnerRole:     OwnerRole,
		OwnerUser:     OwnerUser,
		ReadWriteRole: ReadWriteRole,
		ReadWriteUser: ReadWriteUser,
	}
}

// PSQLArgs returns the psql variable arguments required by SQL.
func (c Config) PSQLArgs() []string {
	return []string{
		"--set=kagent_schema=" + c.Schema,
		"--set=kagent_owner_role=" + c.OwnerRole,
		"--set=kagent_owner_user=" + c.OwnerUser,
		"--set=kagent_owner_password=" + c.OwnerPassword,
		"--set=kagent_readwrite_role=" + c.ReadWriteRole,
		"--set=kagent_readwrite_user=" + c.ReadWriteUser,
		"--set=kagent_readwrite_password=" + c.ReadWritePassword,
	}
}

//go:embed setup.sql
var sql string

// SQL returns the parameterized psql script that creates Kagent's owner and
// read/write roles, their login users, and the schema. The caller supplies Config.PSQLArgs and owns the
// surrounding transaction.
func SQL() string { return sql }

// Script returns the psql script as a complete transaction for direct execution.
func Script() string { return "BEGIN;\n" + sql + "\nCOMMIT;\n" }
