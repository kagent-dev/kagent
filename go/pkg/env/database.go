package env

// Database environment variables.
var (
	GormLogLevel = RegisterStringVar(
		"GORM_LOG_LEVEL",
		"silent",
		"GORM database logging level. Valid values: error, warn, info, silent.",
		ComponentDatabase,
	)
)
