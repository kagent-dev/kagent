package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestRetryDBConnection_DeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Use an unreachable address so every attempt fails immediately.
	_, err := retryDBConnection(ctx, "postgres://user:pass@localhost:1/nodb?connect_timeout=1", &gorm.Config{})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
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
