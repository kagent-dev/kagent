package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	kagentclient "github.com/kagent-dev/kagent/go/api/client"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	// DefaultKAgentURL is the local HTTP endpoint created by the CLI port-forward.
	DefaultKAgentURL = "http://localhost:8083"
	// DefaultKAgentGRPCURL is the local endpoint eligible for automatic port-forward fallback.
	DefaultKAgentGRPCURL = kagentclient.DefaultGRPCTarget
	// DefaultUserID preserves the caller identity used before authentication is configured.
	DefaultUserID = "admin@kagent.dev"
)

type Config struct {
	KAgentURL            string        `mapstructure:"kagent_url"`
	KAgentGRPCURL        string        `mapstructure:"kagent_grpc_url"`
	KAgentGRPCTLS        bool          `mapstructure:"kagent_grpc_tls"`
	KAgentGRPCCAFile     string        `mapstructure:"kagent_grpc_ca_file"`
	KAgentGRPCServerName string        `mapstructure:"kagent_grpc_server_name"`
	Namespace            string        `mapstructure:"namespace"`
	OutputFormat         string        `mapstructure:"output_format"`
	Verbose              bool          `mapstructure:"verbose"`
	Timeout              time.Duration `mapstructure:"timeout"`
	UserID               string        `mapstructure:"user_id"`
}

func (c *Config) Client() *kagentclient.ClientSet {
	options := []kagentclient.ClientOption{
		kagentclient.WithUserID(c.UserID),
	}
	if c.KAgentGRPCURL != "" {
		options = append(options, kagentclient.WithGRPCTarget(c.KAgentGRPCURL))
	}
	if c.Timeout > 0 {
		options = append(options, kagentclient.WithGRPCTimeout(c.Timeout))
	}
	if c.KAgentGRPCTLS {
		options = append(options, kagentclient.WithGRPCTLS(kagentclient.GRPCTLSConfig{
			CAFile:     c.KAgentGRPCCAFile,
			ServerName: c.KAgentGRPCServerName,
		}))
	}
	return kagentclient.New(c.KAgentURL, options...)
}

// Validate rejects configuration that cannot be transported safely.
func (c *Config) Validate() error {
	if c.UserID == "" {
		return errors.New("caller identity is required")
	}
	if strings.IndexFunc(c.UserID, unicode.IsSpace) >= 0 {
		return errors.New("caller identity must not contain whitespace")
	}
	return nil
}

func Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("error getting user home directory: %w", err)
	}

	configFile := filepath.Join(home, ".kagent", "config.yaml")

	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")

	pflag.StringVar(&configFile, "config", configFile, "config file (default is $HOME/.kagent/config.yaml)")

	// Set default values
	viper.SetDefault("kagent_url", DefaultKAgentURL)
	viper.SetDefault("kagent_grpc_url", DefaultKAgentGRPCURL)
	viper.SetDefault("kagent_grpc_tls", false)
	viper.SetDefault("output_format", "table")
	viper.SetDefault("namespace", "kagent")
	viper.SetDefault("timeout", 300*time.Second)
	viper.SetDefault("user_id", DefaultUserID)
	viper.MustBindEnv("kagent_url", "KAGENT_URL")
	viper.MustBindEnv("kagent_grpc_url", "KAGENT_GRPC_URL")
	viper.MustBindEnv("kagent_grpc_tls", "KAGENT_GRPC_TLS")
	viper.MustBindEnv("kagent_grpc_ca_file", "KAGENT_GRPC_CA_FILE")
	viper.MustBindEnv("kagent_grpc_server_name", "KAGENT_GRPC_SERVER_NAME")
	viper.MustBindEnv("output_format", "KAGENT_OUTPUT_FORMAT")
	viper.MustBindEnv("user_id", "KAGENT_USER_ID")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}
	return nil
}

func Get() (*Config, error) {
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}
	if config.UserID == "" {
		config.UserID = DefaultUserID
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &config, nil
}
