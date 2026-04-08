package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	ctrl "sigs.k8s.io/controller-runtime"
)

var dbLog = ctrl.Log.WithName("database")

// Manager handles database connection and initialization
type Manager struct {
	db       *gorm.DB
	config   *Config
	initLock sync.Mutex
}

type PostgresConfig struct {
	URL           string
	URLFile       string
	VectorEnabled bool
}

type Config struct {
	PostgresConfig *PostgresConfig
}

const (
	defaultMaxTimeout   = 120 * time.Second
	defaultInitialDelay = 500 * time.Millisecond
	defaultMaxDelay     = 5 * time.Second
)

// retryDBConnection attempts to open a GORM connection, retrying with backoff until
// the context deadline or defaultMaxTimeout is reached.
func retryDBConnection(ctx context.Context, url string, cfg *gorm.Config) (*gorm.DB, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultMaxTimeout)
	defer cancel()

	start := time.Now()
	delay := defaultInitialDelay
	for attempt := 1; ; attempt++ {
		db, err := gorm.Open(postgres.Open(url), cfg)
		if err == nil {
			return db, nil
		}
		dbLog.Info("database not ready, retrying", "attempt", attempt, "delay", delay, "error", err.Error())
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("database not ready after %s: %w", time.Since(start).Round(time.Second), ctx.Err())
		case <-time.After(delay):
		}
		delay *= 2
		if delay > defaultMaxDelay {
			delay = defaultMaxDelay
		}
	}
}

// NewManager creates a new database manager
func NewManager(ctx context.Context, config *Config) (*Manager, error) {
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

	url := config.PostgresConfig.URL
	if config.PostgresConfig.URLFile != "" {
		resolved, err := resolveURLFile(config.PostgresConfig.URLFile)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve postgres URL from file: %w", err)
		}
		url = resolved
	}

	gormCfg := &gorm.Config{
		Logger:         logger.Default.LogMode(logLevel),
		TranslateError: true,
	}

	db, err := retryDBConnection(ctx, url, gormCfg)
	if err != nil {
		return nil, err
	}

	return &Manager{db: db, config: config}, nil
}

// Initialize sets up the database tables
func (m *Manager) Initialize() error {
	if m.config.PostgresConfig.VectorEnabled {
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

	if m.config.PostgresConfig.VectorEnabled {
		if err := m.db.AutoMigrate(&dbpkg.Memory{}); err != nil {
			return fmt.Errorf("failed to migrate memory table: %w", err)
		}

		// Manually create the HNSW index with the correct operator class —
		// GORM doesn't support adding "op class" in struct tags for Postgres vectors.
		indexQuery := `CREATE INDEX IF NOT EXISTS idx_memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops)`
		if err := m.db.Exec(indexQuery).Error; err != nil {
			return fmt.Errorf("failed to create hnsw index: %w", err)
		}
	}

	return nil
}

// Reset drops all tables and optionally recreates them
func (m *Manager) Reset(recreateTables bool) error {
	if !m.initLock.TryLock() {
		return fmt.Errorf("database reset already in progress")
	}
	defer m.initLock.Unlock()

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
		&dbpkg.Memory{},
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
