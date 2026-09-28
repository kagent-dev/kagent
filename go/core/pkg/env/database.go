package env

var (
	PostgresDatabaseURL = RegisterStringVar(
		"KAGENT_POSTGRES_DATABASE_URL", "postgres://postgres:kagent@kagent-postgresql.kagent.svc.cluster.local:5432/postgres",
		"PostgreSQL connection URL or @file:/absolute/path source, reread for each new connection. The default applies only to the controller; kagent db requires this variable or --db-url. Helm supplies its configured connection URL.", ComponentDatabase, ComponentController, ComponentCLI,
	)
	DatabaseBootstrap = RegisterStringVar(
		"KAGENT_DATABASE_BOOTSTRAP", "",
		"Bootstrap bundled PostgreSQL identities before migrations. Accepts true, false, or empty (disabled).", ComponentDatabase, ComponentController,
	)
	PostgresAdminUsernameFile = RegisterStringVar(
		"POSTGRES_ADMIN_USERNAME_FILE", "",
		"File containing the PostgreSQL administrator username. Required when bundled identity bootstrap is enabled.", ComponentDatabase, ComponentController,
	)
	PostgresAdminPasswordFile = RegisterStringVar(
		"POSTGRES_ADMIN_PASSWORD_FILE", "",
		"File containing the PostgreSQL administrator password. Required when bundled identity bootstrap is enabled.", ComponentDatabase, ComponentController,
	)
)
