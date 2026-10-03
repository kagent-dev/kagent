package env

import (
	"runtime"
	"time"
)

var (
	PostgresDatabaseURL = RegisterStringVar(
		"KAGENT_POSTGRES_DATABASE_URL", "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres",
		"PostgreSQL connection URL. The default applies only to the controller; kagent db requires this variable or --db-url. Helm supplies its configured connection URL.", ComponentDatabase, ComponentController, ComponentCLI,
	)
	PostgresDatabaseURLFile = RegisterStringVar(
		"KAGENT_POSTGRES_DATABASE_URL_FILE", "",
		"File containing the PostgreSQL connection URL; takes precedence over KAGENT_POSTGRES_DATABASE_URL in the controller.", ComponentDatabase, ComponentController,
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
