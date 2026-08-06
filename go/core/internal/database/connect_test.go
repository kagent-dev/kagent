package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryDBConnection_DeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := retryDBConnection(ctx, &PostgresConfig{
		URL: "postgres://user:pass@localhost:1/nodb?connect_timeout=1",
	})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestApplyPoolConfig(t *testing.T) {
	base, err := pgxpool.ParseConfig("postgres://user:pass@localhost:5432/db")
	require.NoError(t, err)

	t.Run("unset leaves pgx defaults", func(t *testing.T) {
		config := base.Copy()
		applyPoolConfig(config, &PostgresConfig{})
		assert.Equal(t, base.MaxConns, config.MaxConns)
		assert.Equal(t, int32(0), config.MinConns)
		assert.Equal(t, 30*time.Minute, config.MaxConnIdleTime)
		assert.Equal(t, time.Hour, config.MaxConnLifetime)
	})

	t.Run("set fields override", func(t *testing.T) {
		config := base.Copy()
		maxConns := int32(8)
		minConns := int32(0)
		idle := time.Minute
		lifetime := 10 * time.Minute
		applyPoolConfig(config, &PostgresConfig{
			MaxConns:        &maxConns,
			MinConns:        &minConns,
			MaxConnIdleTime: &idle,
			MaxConnLifetime:  &lifetime,
		})
		assert.Equal(t, int32(8), config.MaxConns)
		assert.Equal(t, int32(0), config.MinConns)
		assert.Equal(t, time.Minute, config.MaxConnIdleTime)
		assert.Equal(t, 10*time.Minute, config.MaxConnLifetime)
	})
}

func TestResolveURLFile(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		wantUrl     string
		wantErr     bool
	}{
		{
			name:        "reads URL from file",
			fileContent: "postgres://testuser:testpass@host:5432/testdb",
			wantUrl:     "postgres://testuser:testpass@host:5432/testdb",
		},
		{
			name:        "trims whitespace and newlines",
			fileContent: "  postgres://user:pass@host:5432/db\n",
			wantUrl:     "postgres://user:pass@host:5432/db",
		},
		{
			name:        "empty file returns error",
			fileContent: "",
			wantErr:     true,
		},
		{
			name:        "whitespace-only file returns error",
			fileContent: "  \n\t\n  ",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), "db-url")
			err := os.WriteFile(tmpFile, []byte(tt.fileContent), 0600)
			assert.NoError(t, err)

			url, err := resolveURLFile(tmpFile)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantUrl, url)
		})
	}

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := resolveURLFile("/nonexistent/path/db-url")
		assert.Error(t, err)
	})
}
