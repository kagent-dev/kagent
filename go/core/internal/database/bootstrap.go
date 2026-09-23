package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

const (
	OwnerRoleName = "kagent_owner"
	UserName      = "kagent_user"
	UserPassword  = "kagent"
)

// BootstrapConfig contains the first-install PostgreSQL credentials.
// EndpointSource supplies the endpoint, database, and TLS configuration.
type BootstrapConfig struct {
	EndpointSource string
	AdminUsername  string
	AdminPassword  string
	Schema         string
	VectorEnabled  bool
	VectorSchema   string
}

// Bootstrap creates the fixed Kagent identity and schema.
// It does not change the password for an existing user.
func Bootstrap(ctx context.Context, cfg BootstrapConfig) error {
	if cfg.EndpointSource == "" {
		return errors.New("PostgreSQL connection string must not be empty")
	}
	if cfg.Schema == "" {
		return errors.New("PostgreSQL schema must not be empty")
	}
	for name, value := range map[string]string{
		"administrator username": cfg.AdminUsername,
		"administrator password": cfg.AdminPassword,
	} {
		if value == "" {
			return fmt.Errorf("PostgreSQL %s must not be empty", name)
		}
	}

	dsn, err := ResolveURL(cfg.EndpointSource)
	if err != nil {
		return err
	}
	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return errors.New("parse PostgreSQL bootstrap connection string: invalid value")
	}
	if connConfig.User != UserName {
		return fmt.Errorf("PostgreSQL bootstrap connection string must contain the %q user", UserName)
	}
	if connConfig.Password != UserPassword {
		return errors.New("PostgreSQL bootstrap connection string does not match the fixed development password")
	}
	connConfig.User = cfg.AdminUsername
	connConfig.Password = cfg.AdminPassword
	conn, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		return fmt.Errorf("connect as PostgreSQL administrator: %w", err)
	}
	defer conn.Close(ctx) //nolint:errcheck // The transaction result decides success.

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start PostgreSQL bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // Commit or the returned error decides the outcome.

	for setting, value := range map[string]string{
		"kagent.bootstrap_username":       UserName,
		"kagent.bootstrap_password":       UserPassword,
		"kagent.bootstrap_owner_role":     OwnerRoleName,
		"kagent.bootstrap_schema":         cfg.Schema,
		"kagent.bootstrap_vector_enabled": fmt.Sprint(cfg.VectorEnabled),
		"kagent.bootstrap_vector_schema":  cfg.VectorSchema,
	} {
		if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, setting, value); err != nil {
			return fmt.Errorf("set PostgreSQL bootstrap parameter %q: %w", setting, err)
		}
	}
	identitySQL, err := migrations.FS.ReadFile("identity/bootstrap.sql")
	if err != nil {
		return fmt.Errorf("read PostgreSQL identity SQL: %w", err)
	}
	if _, err := tx.Conn().PgConn().ExecParams(ctx, string(identitySQL), nil, nil, nil, nil).Close(); err != nil {
		return fmt.Errorf("apply PostgreSQL identity SQL: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit PostgreSQL bootstrap: %w", err)
	}
	return nil
}
