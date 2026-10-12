package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolConfigHidesInvalidURL(t *testing.T) {
	_, err := PoolConfig("postgres://user:password@localhost:5432/database?sslmode=disable", "kagent_owner")
	require.NoError(t, err)

	_, err = PoolConfig("postgres://user:do-not-disclose@[invalid", "kagent_owner")
	require.Error(t, err)
	assert.ErrorIs(t, err, errInvalidURL)
	assert.NotContains(t, err.Error(), "do-not-disclose")
}

func TestPoolConfigRefreshesTLSForLiteralURL(t *testing.T) {
	config, err := PoolConfig("postgres://user:static-password@database:5432/app?sslmode=require", "kagent_owner")
	require.NoError(t, err)
	require.NotNil(t, config.BeforeConnect)

	connConfig := config.ConnConfig.Copy()
	initialTLS := connConfig.TLSConfig
	connConfig.Password = "unchanged-password"
	require.NoError(t, config.BeforeConnect(context.Background(), connConfig))

	assert.Equal(t, "unchanged-password", connConfig.Password)
	assert.NotSame(t, initialTLS, connConfig.TLSConfig)
}

func TestPoolConfigHooks(t *testing.T) {
	config, err := PoolConfig("postgres://user:password@database:5432/app?sslmode=disable", "kagent_owner")
	require.NoError(t, err)
	assert.Nil(t, config.BeforeConnect)
	assert.NotNil(t, config.AfterConnect)
}

func TestPoolConfigRequiresRole(t *testing.T) {
	_, err := PoolConfig("postgres://user:password@database:5432/app?sslmode=disable", "")
	require.ErrorContains(t, err, "PostgreSQL role must not be empty")
}
