package env

import (
	"runtime"
	"time"
)

var (
	PostgresOwnerDatabaseURL = RegisterStringVar(
		"KAGENT_POSTGRES_OWNER_DATABASE_URL", "",
		"PostgreSQL connection URL for the owner login, which creates the schema and runs migrations. Required by the controller unless KAGENT_SKIP_MIGRATIONS is set; kagent db migrate reads it when --db-url is empty. Helm sets it from database.postgres.ownerConnectionStringSecretRef.", ComponentDatabase, ComponentController, ComponentCLI,
	)
	PostgresOwnerRole = RegisterStringVar(
		"KAGENT_POSTGRES_OWNER_ROLE", "",
		"PostgreSQL role the owner login assumes after authentication. Required with KAGENT_POSTGRES_OWNER_DATABASE_URL; kagent db migrate reads it when --db-role is empty.", ComponentDatabase, ComponentController, ComponentCLI,
	)
	PostgresReadWriteDatabaseURL = RegisterStringVar(
		"KAGENT_POSTGRES_READWRITE_DATABASE_URL", "",
		"PostgreSQL connection URL for the read/write login that serves runtime queries. Required by the controller. Helm sets it from database.postgres.readWriteConnectionStringSecretRef.", ComponentDatabase, ComponentController,
	)
	PostgresReadWriteRole = RegisterStringVar(
		"KAGENT_POSTGRES_READWRITE_ROLE", "",
		"PostgreSQL role the read/write login assumes after authentication. Required by the controller.", ComponentDatabase, ComponentController,
	)
	PostgresDatabaseMaxConns        = registerPostgresDatabaseMaxConns()
	PostgresDatabaseMinConns        = RegisterIntVar("KAGENT_POSTGRES_DATABASE_MIN_CONNS", 0, "Minimum size of the PostgreSQL connection pool", ComponentDatabase, ComponentController)
	PostgresDatabaseMaxConnIdleTime = RegisterDurationVar("KAGENT_POSTGRES_DATABASE_MAX_CONN_IDLE_TIME", 30*time.Minute, "Duration after which an idle connection will be automatically closed", ComponentDatabase, ComponentController)
	PostgresDatabaseMaxConnLifetime = RegisterDurationVar("KAGENT_POSTGRES_DATABASE_MAX_CONN_LIFETIME", 1*time.Hour, "Duration since creation after which a connection will be automatically closed", ComponentDatabase, ComponentController)
)

// Special handling for dynamic default value of max conns
func registerPostgresDatabaseMaxConns() IntVar {
	defaultValue := max(4, runtime.NumCPU())
	v := Var{
		Name:         "KAGENT_POSTGRES_DATABASE_MAX_CONNS",
		DefaultValue: "Greater of 4 and number of CPUs",
		Description:  "Maximum size of the PostgreSQL connection pool",
		Type:         TypeInt,
		Components:   []Component{ComponentDatabase, ComponentController},
	}
	register(v)
	return IntVar{v: v, defaultValue: defaultValue}
}
