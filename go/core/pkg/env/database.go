package env

var PostgresDatabaseURL = RegisterStringVar(
	"KAGENT_POSTGRES_DATABASE_URL", "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres",
	"PostgreSQL connection URL or @file:/absolute/path source, reread for each new connection. The default applies only to the controller; kagent db requires this variable or --db-url. Helm supplies its configured connection URL.", ComponentDatabase, ComponentController, ComponentCLI,
)
