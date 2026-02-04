package database

import (
	"fmt"
	"os"
	"sync"

	"github.com/glebarez/sqlite"
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
	DatabasePath string
}

type PostgresConfig struct {
	URL           string
	VectorEnabled bool
}

type Config struct {
	DatabaseType   DatabaseType
	SqliteConfig   *SqliteConfig
	PostgresConfig *PostgresConfig
}

const (
	gormLogLevel = "GORM_LOG_LEVEL"
)

// NewManager creates a new database manager
func NewManager(config *Config) (*Manager, error) {
	var db *gorm.DB
	var err error

	logLevel := logger.Silent
	if val, ok := os.LookupEnv(gormLogLevel); ok {
		switch val {
		case "error":
			logLevel = logger.Error
		case "warn":
			logLevel = logger.Warn
		case "info":
			logLevel = logger.Info
		case "silent":
			logLevel = logger.Silent
		}
	}

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
		&Agent{},
		&Session{},
		&Task{},
		&Event{},
		&PushNotification{},
		&Feedback{},
		&Tool{},
		&ToolServer{},
		&LangGraphCheckpoint{},
		&LangGraphCheckpointWrite{},
		&CrewAIAgentMemory{},
		&CrewAIFlowState{},
	)

	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	if m.config.DatabaseType == DatabaseTypePostgres && m.config.PostgresConfig.VectorEnabled {
		if err := m.db.AutoMigrate(&Memory{}); err != nil {
			return fmt.Errorf("failed to migrate memory table: %w", err)
		}

		// Manually create the HNSW index with the correct operator class
		// GORM doesn't support adding "op class" in struct tags easily for Postgres vectors
		indexQuery := `CREATE INDEX IF NOT EXISTS idx_memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops)`
		if err := m.db.Exec(indexQuery).Error; err != nil {
			return fmt.Errorf("failed to create hnsw index: %w", err)
		}
	}

	// Setup pg_cron for memory TTL cleanup (optional - will silently fail if pg_cron not available)
	// if m.db.Dialector.Name() == "postgres" {
	// 	// Try to enable pg_cron extension (requires superuser, may fail)
	// 	_ = m.db.Exec("CREATE EXTENSION IF NOT EXISTS pg_cron")

	// 	// Schedule hourly cleanup of expired memories
	// 	// This will fail silently if pg_cron is not available or user lacks permissions
	// 	_ = m.db.Exec(`
	// 		SELECT cron.unschedule('cleanup_expired_memories')
	// 		WHERE EXISTS (SELECT 1 FROM cron.job WHERE jobname = 'cleanup_expired_memories')
	// 	`)
	// 	_ = m.db.Exec(`
	// 		SELECT cron.schedule(
	// 			'cleanup_expired_memories',
	// 			'0 * * * *',
	// 			$$DELETE FROM memory WHERE expires_at IS NOT NULL AND expires_at < NOW()$$
	// 		)
	// 	`)
	// }

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
		&Agent{},
		&Session{},
		&Task{},
		&Event{},
		&PushNotification{},
		&Feedback{},
		&Tool{},
		&ToolServer{},
		&LangGraphCheckpoint{},
		&LangGraphCheckpointWrite{},
		&CrewAIAgentMemory{},
		&CrewAIFlowState{},
	)

	if err != nil {
		return fmt.Errorf("failed to drop tables: %w", err)
	}

	if recreateTables {
		return m.Initialize()
	}

	return nil
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
