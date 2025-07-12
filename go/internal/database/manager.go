package database

import (
	"fmt"
	"sync"

	"github.com/glebarez/sqlite"
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
	URL string
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

	switch config.DatabaseType {
	case DatabaseTypeSqlite:
		db, err = gorm.Open(sqlite.Open(config.SqliteConfig.DatabasePath), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Warn),
		})
	case DatabaseTypePostgres:
		db, err = gorm.Open(postgres.Open(config.PostgresConfig.URL), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Warn),
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
		&Agent{},
		&Session{},
		&Task{},
		&Message{},
		&PushNotification{},
		&Feedback{},
		&Tool{},
		&ToolServer{},
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
		&Agent{},
		&Session{},
		&Task{},
		&Message{},
		&PushNotification{},
		&Feedback{},
		&Tool{},
		&ToolServer{},
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
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
