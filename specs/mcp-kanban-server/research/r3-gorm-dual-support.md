# R3: GORM with SQLite + Postgres Dual Support

## Pattern — Already in Kagent

The exact pattern needed is in `go/internal/database/manager.go`. Reuse it verbatim.

```go
type DatabaseType string

const (
    DatabaseTypeSqlite   DatabaseType = "sqlite"
    DatabaseTypePostgres DatabaseType = "postgres"
)

switch config.DatabaseType {
case DatabaseTypeSqlite:
    db, err = gorm.Open(sqlite.Open(config.SqliteConfig.DatabasePath), &gorm.Config{
        Logger:         logger.Default.LogMode(logLevel),
        TranslateError: true,
    })
case DatabaseTypePostgres:
    db, err = gorm.Open(postgres.Open(config.PostgresConfig.URL), &gorm.Config{
        Logger:         logger.Default.LogMode(logLevel),
        TranslateError: true,
    })
}
```

## Import Paths (already in go/go.mod)
```go
"github.com/glebarez/sqlite"   // pure Go, no CGO — already used in kagent
"gorm.io/driver/postgres"      // already in go.mod v1.6.0
"gorm.io/gorm"
```

## AutoMigrate
```go
func (m *Manager) Initialize() error {
    return m.db.AutoMigrate(&Task{}, &Board{})
}
```

## Notes
- `TranslateError: true` normalizes "record not found" etc. across SQLite and Postgres
- Default config: SQLite at `kanban.db` in working directory
- Postgres: full DSN URL via `--postgres-url` flag or env var
