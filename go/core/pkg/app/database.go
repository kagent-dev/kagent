package app

import (
	"fmt"
	"math"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

func databaseConfigFromEnv() (database.PostgresConfig, error) {
	var config database.PostgresConfig
	for _, setting := range []struct {
		variable kagentenv.IntVar
		target   **int32
		minimum  int
	}{
		{kagentenv.DBMaxConns, &config.MaxConns, 1},
		{kagentenv.DBMinConns, &config.MinConns, 0},
	} {
		value, set, err := setting.variable.LookupWithError()
		if err != nil {
			return config, err
		}
		if set {
			if value < setting.minimum || value > math.MaxInt32 {
				return config, fmt.Errorf("%s must be between %d and %d", setting.variable.Name(), setting.minimum, math.MaxInt32)
			}
			*setting.target = new(int32(value))
		}
	}
	for _, setting := range []struct {
		variable kagentenv.DurationVar
		target   **time.Duration
	}{
		{kagentenv.DBMaxConnIdleTime, &config.MaxConnIdleTime},
		{kagentenv.DBMaxConnLifetime, &config.MaxConnLifetime},
	} {
		value, set, err := setting.variable.LookupWithError()
		if err != nil {
			return config, err
		}
		if set {
			if value <= 0 {
				return config, fmt.Errorf("%s must be positive", setting.variable.Name())
			}
			*setting.target = &value
		}
	}
	return config, nil
}
