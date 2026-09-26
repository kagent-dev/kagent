package database

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapCreatesManagedIdentity(t *testing.T) {
	const schema = "kagent_bootstrap_test"
	cleanupBootstrap(t, schema)
	t.Cleanup(func() { cleanupBootstrap(t, schema) })

	adminConfig, err := pgx.ParseConfig(sharedConnStr)
	require.NoError(t, err)
	dsn, err := url.Parse(sharedConnStr)
	require.NoError(t, err)
	dsn.User = url.UserPassword(UserName, UserPassword)
	bootstrapDSN := *dsn
	query := bootstrapDSN.Query()
	query.Set("pool_max_conns", "4")
	bootstrapDSN.RawQuery = query.Encode()
	cfg := BootstrapConfig{
		EndpointSource: bootstrapDSN.String(),
		AdminUsername:  adminConfig.User,
		AdminPassword:  adminConfig.Password,
		Schema:         schema,
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- Bootstrap(t.Context(), cfg) }()
	}
	for range 2 {
		require.NoError(t, <-errs)
	}

	var owner string
	require.NoError(t, sharedDB.QueryRow(t.Context(),
		`SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = $1`, schema).Scan(&owner))
	assert.Equal(t, OwnerRoleName, owner)

	var member bool
	require.NoError(t, sharedDB.QueryRow(t.Context(),
		`SELECT pg_has_role($1, $2, 'MEMBER')`, UserName, OwnerRoleName).Scan(&member))
	assert.True(t, member)

	// A retry validates the identity. It must not reset a password changed by an operator.
	_, err = sharedDB.Exec(t.Context(), `ALTER ROLE kagent_user PASSWORD 'replacement-password'`)
	require.NoError(t, err)
	require.NoError(t, Bootstrap(t.Context(), BootstrapConfig{
		EndpointSource: bootstrapDSN.String(),
		AdminUsername:  adminConfig.User,
		AdminPassword:  adminConfig.Password,
		Schema:         schema,
	}))

	rotated := *dsn
	rotated.User = url.UserPassword(UserName, "replacement-password")
	conn, err := pgx.Connect(t.Context(), rotated.String())
	require.NoError(t, err)
	defer conn.Close(t.Context()) //nolint:errcheck
	_, err = conn.Exec(t.Context(), "SET ROLE "+pgx.Identifier{OwnerRoleName}.Sanitize())
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), fmt.Sprintf(`CREATE TABLE %s.bootstrap_data (id integer)`,
		pgx.Identifier{schema}.Sanitize()))
	require.NoError(t, err)

	_, err = sharedDB.Exec(t.Context(), `
		DROP SCHEMA IF EXISTS substrate_isolation_test CASCADE;
		CREATE SCHEMA substrate_isolation_test;
		REVOKE ALL ON SCHEMA substrate_isolation_test FROM PUBLIC;
		CREATE TABLE substrate_isolation_test.private_data (id integer)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = sharedDB.Exec(context.Background(), `DROP SCHEMA IF EXISTS substrate_isolation_test CASCADE`)
	})
	_, err = conn.Exec(t.Context(), `SELECT * FROM substrate_isolation_test.private_data`)
	require.Error(t, err)
}

func TestBootstrapAndMigrateInPublicSchema(t *testing.T) {
	const databaseName = "kagent_public_bootstrap_test"
	_, err := sharedDB.Exec(t.Context(), "CREATE DATABASE "+databaseName)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := sharedDB.Exec(context.Background(), "DROP DATABASE "+databaseName+" WITH (FORCE)")
		require.NoError(t, err)
		_, err = sharedDB.Exec(context.Background(), "DROP ROLE IF EXISTS "+UserName)
		require.NoError(t, err)
		_, err = sharedDB.Exec(context.Background(), "DROP ROLE IF EXISTS "+OwnerRoleName)
		require.NoError(t, err)
	})

	dsn, err := url.Parse(sharedConnStr)
	require.NoError(t, err)
	dsn.Path = "/" + databaseName
	adminConfig, err := pgx.ParseConfig(dsn.String())
	require.NoError(t, err)
	dsn.User = url.UserPassword(UserName, UserPassword)
	require.NoError(t, Bootstrap(t.Context(), BootstrapConfig{
		EndpointSource: dsn.String(),
		AdminUsername:  adminConfig.User,
		AdminPassword:  adminConfig.Password,
		Schema:         "public",
	}))
	require.NoError(t, migrations.RunUpAsRole(t.Context(), dsn.String(), OwnerRoleName,
		migrations.BuiltinSourcesInSchema(false, "public", "public")))

	conn, err := pgx.Connect(t.Context(), dsn.String())
	require.NoError(t, err)
	defer conn.Close(t.Context()) //nolint:errcheck
	_, err = conn.Exec(t.Context(), "SET ROLE "+OwnerRoleName)
	require.NoError(t, err)
	var migrated bool
	require.NoError(t, conn.QueryRow(t.Context(),
		"SELECT to_regclass('public.schema_migrations') IS NOT NULL").Scan(&migrated))
	require.True(t, migrated)
}

func TestBootstrapSharesPgvectorAcrossSchemas(t *testing.T) {
	const databaseName = "kagent_shared_vector_test"
	_, err := sharedDB.Exec(t.Context(), "CREATE DATABASE "+databaseName)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := sharedDB.Exec(context.Background(), "DROP DATABASE "+databaseName+" WITH (FORCE)")
		require.NoError(t, err)
		_, err = sharedDB.Exec(context.Background(), "DROP ROLE IF EXISTS "+UserName)
		require.NoError(t, err)
		_, err = sharedDB.Exec(context.Background(), "DROP ROLE IF EXISTS "+OwnerRoleName)
		require.NoError(t, err)
	})

	adminDSN, err := url.Parse(sharedConnStr)
	require.NoError(t, err)
	adminDSN.Path = "/" + databaseName
	adminConfig, err := pgx.ParseConfig(adminDSN.String())
	require.NoError(t, err)
	appDSN := *adminDSN
	appDSN.User = url.UserPassword(UserName, UserPassword)
	for _, schema := range []string{"tenant_one", "tenant_two"} {
		require.NoError(t, Bootstrap(t.Context(), BootstrapConfig{
			EndpointSource: appDSN.String(),
			AdminUsername:  adminConfig.User,
			AdminPassword:  adminConfig.Password,
			Schema:         schema,
			VectorEnabled:  true,
		}))
		require.NoError(t, migrations.RunUpAsRole(t.Context(), appDSN.String(), OwnerRoleName,
			migrations.BuiltinSourcesInSchema(true, schema, "extensions")))
	}

	conn, err := pgx.Connect(t.Context(), adminDSN.String())
	require.NoError(t, err)
	defer conn.Close(t.Context()) //nolint:errcheck
	var vectorSchema string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'vector'`).Scan(&vectorSchema))
	assert.Equal(t, "extensions", vectorSchema)
	for _, schema := range []string{"tenant_one", "tenant_two"} {
		var exists bool
		require.NoError(t, conn.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", schema+".memory").Scan(&exists))
		assert.True(t, exists, "memory table in %s", schema)
	}
	wrongSchema := Bootstrap(t.Context(), BootstrapConfig{
		EndpointSource: appDSN.String(), AdminUsername: adminConfig.User, AdminPassword: adminConfig.Password,
		Schema: "tenant_three", VectorEnabled: true, VectorSchema: "public",
	})
	require.ErrorContains(t, wrongSchema, `pgvector is installed in schema "extensions", expected "public"`)
	var thirdSchemaExists bool
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'tenant_three')").Scan(&thirdSchemaExists))
	assert.False(t, thirdSchemaExists)

	pool, err := Connect(t.Context(), &PostgresConfig{
		URL: appDSN.String(), Role: OwnerRoleName, Schema: "tenant_two", VectorEnabled: true,
	})
	require.NoError(t, err)
	defer pool.Close()
	var currentSchema string
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT current_schema()").Scan(&currentSchema))
	assert.Equal(t, "tenant_two", currentSchema)
	var searchPath string
	require.NoError(t, pool.QueryRow(t.Context(), "SHOW search_path").Scan(&searchPath))
	assert.Equal(t, `"tenant_two"`, searchPath)
	client := NewClient(pool, "extensions")
	memory := &Memory{AgentName: "agent", UserID: "user", Content: "test", Embedding: makeEmbedding(1)}
	require.NoError(t, client.StoreAgentMemories(t.Context(), memory))
	results, err := client.SearchAgentMemory(t.Context(), "agent", "user", makeEmbedding(1), 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, memory.ID, results[0].ID)
}

func TestBootstrapRequiresConnectionString(t *testing.T) {
	err := Bootstrap(t.Context(), BootstrapConfig{})
	require.ErrorContains(t, err, "connection string must not be empty")
}

func TestBootstrapRejectsCustomLogin(t *testing.T) {
	err := Bootstrap(t.Context(), BootstrapConfig{
		EndpointSource: "postgresql://custom:password@localhost/kagent",
		AdminUsername:  "postgres",
		AdminPassword:  "postgres",
		Schema:         "kagent",
	})
	require.ErrorContains(t, err, `must contain the "kagent_user" user`)
}

func TestBootstrapRejectsCustomPassword(t *testing.T) {
	err := Bootstrap(t.Context(), BootstrapConfig{
		EndpointSource: "postgresql://kagent_user:custom@localhost/kagent",
		AdminUsername:  "postgres",
		AdminPassword:  "postgres",
		Schema:         "public",
	})
	require.ErrorContains(t, err, "does not match the fixed development password")
}

func TestIdentitySQLSupportsOperatorLogin(t *testing.T) {
	const (
		schema   = "kagent_operator_identity_test"
		username = "operator's-login"
		password = "operator's-password"
		role     = "operator's-owner"
	)
	cleanupBootstrap(t, schema)
	t.Cleanup(func() { cleanupBootstrap(t, schema) })
	t.Cleanup(func() {
		_, err := sharedDB.Exec(context.Background(), fmt.Sprintf(`
			DROP SCHEMA IF EXISTS %s CASCADE;
			DROP ROLE IF EXISTS %s;
			DROP ROLE IF EXISTS %s`,
			pgx.Identifier{schema}.Sanitize(),
			pgx.Identifier{username}.Sanitize(),
			pgx.Identifier{role}.Sanitize()))
		require.NoError(t, err)
	})

	identitySQL, err := migrations.FS.ReadFile("identity/bootstrap.sql")
	require.NoError(t, err)
	tx, err := sharedDB.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(t.Context()) //nolint:errcheck
	for setting, value := range map[string]string{
		"kagent.bootstrap_username":       username,
		"kagent.bootstrap_password":       password,
		"kagent.bootstrap_schema":         schema,
		"kagent.bootstrap_owner_role":     role,
		"kagent.bootstrap_vector_enabled": "false",
	} {
		_, err := tx.Exec(t.Context(), `SELECT set_config($1, $2, true)`, setting, value)
		require.NoError(t, err)
	}
	_, err = tx.Exec(t.Context(), string(identitySQL))
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	var schemaOwner string
	require.NoError(t, sharedDB.QueryRow(t.Context(),
		`SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = $1`, schema).Scan(&schemaOwner))
	assert.Equal(t, role, schemaOwner)

	connConfig, err := pgx.ParseConfig(sharedConnStr)
	require.NoError(t, err)
	connConfig.User, connConfig.Password = username, password
	conn, err := pgx.ConnectConfig(t.Context(), connConfig)
	require.NoError(t, err)
	defer conn.Close(t.Context()) //nolint:errcheck
	_, err = conn.Exec(t.Context(), "SET ROLE "+pgx.Identifier{role}.Sanitize())
	require.NoError(t, err)
}

func cleanupBootstrap(t *testing.T, schema string) {
	t.Helper()
	ctx := context.Background()
	_, err := sharedDB.Exec(ctx, fmt.Sprintf(`
		DROP SCHEMA IF EXISTS %s CASCADE;
		DROP ROLE IF EXISTS %s;
		DROP ROLE IF EXISTS %s`,
		pgx.Identifier{schema}.Sanitize(),
		pgx.Identifier{UserName}.Sanitize(),
		pgx.Identifier{OwnerRoleName}.Sanitize()))
	require.NoError(t, err)
}
