package env

import (
	"runtime"
	"time"
)

var (
	PostgresDatabaseURL = RegisterStringVar(
		"KAGENT_POSTGRES_DATABASE_URL", "",
		"PostgreSQL connection URL. Required by the controller; kagent db reads it when --db-url is empty. Helm sets it from database.postgres.connectionStringSecretRef.", ComponentDatabase, ComponentController, ComponentCLI,
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
