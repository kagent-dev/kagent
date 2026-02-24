package database

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/glebarez/sqlite"
	dbpkg "github.com/kagent-dev/kagent/go/pkg/database"
	"github.com/kagent-dev/kagent/go/pkg/env"
	_ "turso.tech/database/tursogo"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Manager handles database connection and initialization
type Manager struct {
	db       *gorm.DB
	config   *Config
	initLock sync.Mutex
}

type DatabaseType string

const (
	DatabaseTypeSqlite   DatabaseType = "sqlite"
	DatabaseTypePostgres DatabaseType = "postgres"
)

type SqliteConfig struct {
	DatabasePath  string
	VectorEnabled bool
}

type PostgresConfig struct {
	URL           string
	URLFile       string
	VectorEnabled bool
}

type Config struct {
	DatabaseType   DatabaseType
	SqliteConfig   *SqliteConfig
	PostgresConfig *PostgresConfig
}

// NewManager creates a new database manager
func NewManager(config *Config) (*Manager, error) {
	var db *gorm.DB
	var err error

	logLevel := logger.Silent
	switch env.GormLogLevel.Get() {
	case "error":
		logLevel = logger.Error
	case "warn":
		logLevel = logger.Warn
	case "info":
		logLevel = logger.Info
	case "silent":
		logLevel = logger.Silent
	}

	switch config.DatabaseType {
	case DatabaseTypeSqlite:
		// GORM uses glebarez/sqlite as dialector over the connection
		// The actual driver is Turso's tursogo driver
		sqlDB, sqlErr := sql.Open("turso", config.SqliteConfig.DatabasePath)
		if sqlErr != nil {
			return nil, fmt.Errorf("failed to open turso connection: %w", sqlErr)
		}
		sqlDB.SetMaxOpenConns(1)
		db, err = gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{
			Logger:         logger.Default.LogMode(logLevel),
			TranslateError: true,
		})
	case DatabaseTypePostgres:
		url := config.PostgresConfig.URL
		if config.PostgresConfig.URLFile != "" {
			resolved, resolveErr := resolveURLFile(config.PostgresConfig.URLFile)
			if resolveErr != nil {
				return nil, fmt.Errorf("failed to resolve postgres URL from file: %w", resolveErr)
			}
			url = resolved
		}
		db, err = gorm.Open(postgres.Open(url), &gorm.Config{
			Logger:         logger.Default.LogMode(logLevel),
			TranslateError: true,
		})
	default:
		return nil, fmt.Errorf("invalid database type: %s", config.DatabaseType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &Manager{db: db, config: config}, nil
}

// Initialize sets up the database tables
func (m *Manager) Initialize() error {
	// Create extensions if using Postgres and Vector is enabled
	if m.db.Name() == "postgres" && m.config.PostgresConfig.VectorEnabled {
		if err := m.db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
			return fmt.Errorf("failed to create vector extension: %w", err)
		}
	}

	// AutoMigrate all models
	err := m.db.AutoMigrate(
		&dbpkg.Agent{},
		&dbpkg.Session{},
		&dbpkg.Task{},
		&dbpkg.Event{},
		&dbpkg.PushNotification{},
		&dbpkg.Feedback{},
		&dbpkg.Tool{},
		&dbpkg.ToolServer{},
		&dbpkg.LangGraphCheckpoint{},
		&dbpkg.LangGraphCheckpointWrite{},
		&dbpkg.CrewAIAgentMemory{},
		&dbpkg.CrewAIFlowState{},
	)

	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// DatabaseTypePostgres check for Memory table
	if m.config.DatabaseType == DatabaseTypePostgres && m.config.PostgresConfig.VectorEnabled {
		if err := m.db.AutoMigrate(&dbpkg.Memory{}); err != nil {
			return fmt.Errorf("failed to migrate memory table: %w", err)
		}

		// Manually create the HNSW index with the correct operator class
		// GORM doesn't support adding "op class" in struct tags easily for Postgres vectors
		indexQuery := `CREATE INDEX IF NOT EXISTS idx_memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops)`
		if err := m.db.Exec(indexQuery).Error; err != nil {
			return fmt.Errorf("failed to create hnsw index: %w", err)
		}
	}

	// DatabaseTypeSqlite check for Memory table
	// libSQL uses F32_BLOB(N) for vector columns, not vector(N) like pgvector
	// AutoMigrate doesn't work because GORM tries to use the pgvector type from struct tags
	if m.config.DatabaseType == DatabaseTypeSqlite && m.config.SqliteConfig.VectorEnabled {
		createMemoryTableSQL := `
			CREATE TABLE IF NOT EXISTS memory (
				id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
				agent_name TEXT,
				user_id TEXT,
				content TEXT,
				embedding F32_BLOB(768),
				metadata TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				expires_at DATETIME,
				access_count INTEGER DEFAULT 0
			)
		`
		if err := m.db.Exec(createMemoryTableSQL).Error; err != nil {
			return fmt.Errorf("failed to create memory table: %w", err)
		}
		// Create indexes
		_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_memory_agent_name ON memory(agent_name)`)
		_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_memory_user_id ON memory(user_id)`)
		_ = m.db.Exec(`CREATE INDEX IF NOT EXISTS idx_memory_expires_at ON memory(expires_at)`)
	}

	return nil
}

// Reset drops all tables and optionally recreates them
func (m *Manager) Reset(recreateTables bool) error {
	if !m.initLock.TryLock() {
		return fmt.Errorf("database reset already in progress")
	}
	defer m.initLock.Unlock()

	// Drop all tables
	err := m.db.Migrator().DropTable(
		&dbpkg.Agent{},
		&dbpkg.Session{},
		&dbpkg.Task{},
		&dbpkg.Event{},
		&dbpkg.PushNotification{},
		&dbpkg.Feedback{},
		&dbpkg.Tool{},
		&dbpkg.ToolServer{},
		&dbpkg.LangGraphCheckpoint{},
		&dbpkg.LangGraphCheckpointWrite{},
		&dbpkg.CrewAIAgentMemory{},
		&dbpkg.CrewAIFlowState{},
	)

	if err != nil {
		return fmt.Errorf("failed to drop tables: %w", err)
	}

	if recreateTables {
		return m.Initialize()
	}

	return nil
}

// resolveURLFile reads a database connection URL from a file and returns the
// trimmed contents. Returns an error if the file cannot be read or is empty.
func resolveURLFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading URL file: %w", err)
	}
	url := strings.TrimSpace(string(content))
	if url == "" {
		return "", fmt.Errorf("URL file %s is empty or contains only whitespace", path)
	}
	return url, nil
}

// Close closes the database connection
func (m *Manager) Close() error {
	if m.db == nil {
		return nil
	}

	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
