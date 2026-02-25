package db

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"github.com/kagent-dev/kagent/go/cmd/kanban-mcp/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Manager handles database connection and initialization.
type Manager struct {
	db *gorm.DB
}

// NewManager creates a new database manager based on the provided config.
func NewManager(cfg *config.Config) (*Manager, error) {
	var db *gorm.DB
	var err error

	gormCfg := &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	}

	switch cfg.DBType {
	case config.DBTypeSQLite:
		db, err = gorm.Open(sqlite.Open(cfg.DBPath), gormCfg)
	case config.DBTypePostgres:
		db, err = gorm.Open(postgres.Open(cfg.DBURL), gormCfg)
	default:
		return nil, fmt.Errorf("invalid database type: %s", cfg.DBType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &Manager{db: db}, nil
}

// Initialize runs AutoMigrate for the Task and Attachment models.
func (m *Manager) Initialize() error {
	if err := m.db.AutoMigrate(&Task{}, &Attachment{}); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}
	return nil
}

// DB returns the underlying *gorm.DB instance.
func (m *Manager) DB() *gorm.DB {
	return m.db
}
