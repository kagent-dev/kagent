package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
