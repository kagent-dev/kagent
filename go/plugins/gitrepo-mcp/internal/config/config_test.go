package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadArgs_Defaults(t *testing.T) {
	cfg, err := LoadArgs([]string{})
	require.NoError(t, err)

	assert.Equal(t, ":8090", cfg.Addr)
	assert.Equal(t, "http", cfg.Transport)
	assert.Equal(t, DBTypeSQLite, cfg.DBType)
	assert.Equal(t, "./data/gitrepo.db", cfg.DBPath)
	assert.Equal(t, "", cfg.DBURL)
	assert.Equal(t, "./data", cfg.DataDir)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestLoadArgs_CustomFlags(t *testing.T) {
	cfg, err := LoadArgs([]string{
		"--addr", ":9090",
		"--transport", "stdio",
		"--db-type", "postgres",
		"--db-url", "postgres://localhost:5432/test",
		"--data-dir", "/custom/data",
		"--log-level", "debug",
	})
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.Addr)
	assert.Equal(t, "stdio", cfg.Transport)
	assert.Equal(t, DBTypePostgres, cfg.DBType)
	assert.Equal(t, "postgres://localhost:5432/test", cfg.DBURL)
	assert.Equal(t, "/custom/data", cfg.DataDir)
	assert.Equal(t, "debug", cfg.LogLevel)
}

func TestLoadArgs_EnvVarFallback(t *testing.T) {
	t.Setenv("GITREPO_ADDR", ":7070")
	t.Setenv("GITREPO_DATA_DIR", "/env/data")

	cfg, err := LoadArgs([]string{})
	require.NoError(t, err)

	assert.Equal(t, ":7070", cfg.Addr)
	assert.Equal(t, "/env/data", cfg.DataDir)
}

func TestLoadArgs_FlagsOverrideEnv(t *testing.T) {
	t.Setenv("GITREPO_ADDR", ":7070")

	cfg, err := LoadArgs([]string{"--addr", ":9090"})
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.Addr)
}
