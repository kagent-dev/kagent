## Database migration best practices

### GORM AutoMigrate Approach

Kagent uses GORM's AutoMigrate feature instead of traditional migration files:

From `go/internal/database/service.go` (lines 112-125):

```go
var migrationMutex sync.Mutex

func (s *Service) MigrateModels() error {
    migrationMutex.Lock()
    defer migrationMutex.Unlock()

    return s.db.AutoMigrate(
        &Agent{},
        &Session{},
        &Task{},
        &ModelConfig{},
        &RemoteMCPServer{},
        &Checkpoint{},
        &CheckpointWrite{},
    )
}
```

### How AutoMigrate Works

- **Additive Only**: AutoMigrate adds missing tables, columns, and indexes
- **No Destructive Changes**: Does NOT modify existing columns or delete tables/columns
- **Idempotent**: Safe to run multiple times - only applies missing changes
- **No Rollback**: Does not support down migrations

### Migration Execution

**1. Application Startup**

Migrations run automatically when the application starts:

```go
func main() {
    // Initialize database
    db, err := database.New(config)
    if err != nil {
        log.Fatal(err)
    }

    // Run migrations
    if err := db.MigrateModels(); err != nil {
        log.Fatal("Failed to migrate models:", err)
    }
}
```

**2. Mutex Protection**

A mutex prevents concurrent migrations from multiple instances:

```go
var migrationMutex sync.Mutex

migrationMutex.Lock()
defer migrationMutex.Unlock()
```

### Schema Evolution

**Adding New Models**

1. Define the model struct in `models.go`:
```go
type NewResource struct {
    gorm.Model
    Name   string `gorm:"not null"`
    UserID string `gorm:"index"`
}

func (NewResource) TableName() string {
    return "new_resources"
}
```

2. Add to AutoMigrate list:
```go
db.AutoMigrate(
    &Agent{},
    &NewResource{},  // Add here
    // ... other models
)
```

**Adding New Fields**

1. Add field to existing struct:
```go
type Agent struct {
    gorm.Model
    Name        string `gorm:"not null"`
    NewField    string `gorm:"index"`  // New field
}
```

2. AutoMigrate will add the column on next startup

**Adding Indexes**

1. Add index tag to struct field:
```go
UserID string `gorm:"index"`  // Simple index
```

2. Or use composite unique index:
```go
Namespace string `gorm:"not null;index:idx_agent_namespace_name,unique"`
Name      string `gorm:"not null;index:idx_agent_namespace_name,unique"`
```

### Schema Change Limitations

**Cannot Automatically Handle:**
- Renaming columns (will create new column)
- Changing column types (requires manual migration)
- Removing columns (must be done manually)
- Renaming tables
- Complex data transformations

**For Complex Changes:**

Use raw SQL migrations when needed:

```go
func (s *Service) CustomMigration() error {
    return s.db.Exec(`
        -- Manual migration SQL
        ALTER TABLE agents ADD COLUMN IF NOT EXISTS new_column TEXT;
        UPDATE agents SET new_column = old_column WHERE condition;
        -- etc.
    `).Error
}
```

### Database Support

**SQLite (Development)**
- File-based database
- Good for local development
- Limited concurrent writes

**PostgreSQL (Production)**
- Full-featured RDBMS
- Better concurrent access
- Advanced JSON query support

### Migration Order

Models should be migrated in dependency order (if foreign keys exist):

```go
db.AutoMigrate(
    &Parent{},      // First
    &Child{},       // After parent
)
```

Note: Kagent models mostly avoid foreign keys, so order is less critical.

### Best Practices

1. **Test Migrations Locally**: Always test AutoMigrate with your changes locally first
2. **Review Generated SQL**: Check GORM logs to see what SQL is executed
3. **Backup Before Production**: Take database backup before deploying schema changes
4. **Gradual Rollout**: Deploy schema changes separately from code that requires them
5. **Additive Changes**: Keep changes additive (add columns, don't rename/remove)
6. **Default Values**: Use GORM tags for default values: `gorm:"default:value"`
7. **Nullable First**: Add new columns as nullable first, populate data, then add NOT NULL if needed

### Logging Migrations

Enable GORM logging to see migration SQL:

```go
db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
    Logger: logger.Default.LogMode(logger.Info),
})
```

This shows:
- Tables created
- Columns added
- Indexes created

### Version Control

- **Commit Model Changes**: Always commit model struct changes to git
- **Document Breaking Changes**: Use comments or docs for any manual migration needs
- **Tag Releases**: Tag releases that include schema changes for rollback reference
