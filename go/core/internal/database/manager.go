package database

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/glebarez/sqlite"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Manager handles database connection and initialization
type Manager struct {
	db       *gorm.DB
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
	URL     string
	URLFile string
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
		db, err = gorm.Open(sqlite.Open(config.SqliteConfig.DatabasePath), &gorm.Config{
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

	return &Manager{db: db}, nil
}

// Initialize sets up the database tables
func (m *Manager) Initialize() error {
	if !m.initLock.TryLock() {
		return fmt.Errorf("database initialization already in progress")
	}
	defer m.initLock.Unlock()

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
