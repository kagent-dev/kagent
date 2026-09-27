package env

var (
	PostgresDatabaseURL = RegisterStringVar(
		"POSTGRES_DATABASE_URL", "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres",
		"PostgreSQL connection URL. The default applies only to the controller; kagent db requires this variable or --db-url. Helm supplies its configured connection URL.", ComponentDatabase,
	)
	PostgresDatabaseURLFile = RegisterStringVar(
		"POSTGRES_DATABASE_URL_FILE", "",
		"File containing the PostgreSQL connection URL; takes precedence over POSTGRES_DATABASE_URL in the controller.", ComponentDatabase,
	)
	_ = RegisterStringVar("POSTGRES_PASSWORD", "", "Password supplied from the chart-managed Secret to bundled PostgreSQL and expanded into the controller's POSTGRES_DATABASE_URL by Kubernetes.", ComponentDatabase)
	_ = RegisterStringVar("POSTGRES_DB", "", "Initial database name for bundled PostgreSQL, supplied by the Helm chart.", ComponentDatabase)
	_ = RegisterStringVar("POSTGRES_USER", "", "Initial user for bundled PostgreSQL, supplied by the Helm chart.", ComponentDatabase)
	_ = RegisterStringVar("PGDATA", "", "Data directory for bundled PostgreSQL, supplied by the Helm chart.", ComponentDatabase)
)
