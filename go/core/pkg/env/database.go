package env

import "time"

var (
	PostgresDatabaseURL = RegisterStringVar(
		"KAGENT_POSTGRES_DATABASE_URL", "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres",
		"PostgreSQL connection URL. The default applies only to the controller; kagent db requires this variable or --db-url. Helm supplies its configured connection URL.", ComponentDatabase, ComponentController, ComponentCLI,
	)
	PostgresDatabaseURLFile = RegisterStringVar(
		"KAGENT_POSTGRES_DATABASE_URL_FILE", "",
		"File containing the PostgreSQL connection URL; takes precedence over KAGENT_POSTGRES_DATABASE_URL in the controller.", ComponentDatabase, ComponentController,
	)
	PostgresDatabaseMaxConns        = RegisterIntVar("DB_MAX_CONNS", 4, "Maximum size of the PostgreSQL connection pool", ComponentDatabase, ComponentController)
	PostgresDatabaseMinConns        = RegisterIntVar("DB_MIN_CONNS", 0, "Minimum size of the PostgreSQL connection pool", ComponentDatabase, ComponentController)
	PostgresDatabaseMaxConnIdleTime = RegisterDurationVar("DB_MAX_CONN_IDLE_TIME", 30*time.Minute, "Duration after which an idle connection will be automatically closed", ComponentDatabase, ComponentController)
	PostgresDatabaseMaxConnLifetime = RegisterDurationVar("DB_MAX_CONN_LIFETIME", 1*time.Hour, "Duration since creation after which a connection will be automatically closed", ComponentDatabase, ComponentController)
)
