package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	kagentclient "github.com/kagent-dev/kagent/go/api/client"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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
}

func (c *Config) Client() *kagentclient.ClientSet {
	options := []kagentclient.ClientOption{
		kagentclient.WithUserID("admin@kagent.dev"),
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

func Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("error getting user home directory: %w", err)
	}

	configDir := filepath.Join(home, ".kagent")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("error creating config directory: %w", err)
	}

	configFile := filepath.Join(configDir, "config.yaml")

	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")

	pflag.StringVar(&configFile, "config", configFile, "config file (default is $HOME/.kagent/config.yaml)")

	// Set default values
	viper.SetDefault("kagent_url", "http://localhost:8083")
	viper.SetDefault("kagent_grpc_url", "localhost:8084")
	viper.SetDefault("kagent_grpc_tls", false)
	viper.SetDefault("output_format", "table")
	viper.SetDefault("namespace", "kagent")
	viper.SetDefault("timeout", 300*time.Second)
	viper.MustBindEnv("kagent_url", "KAGENT_URL")
	viper.MustBindEnv("kagent_grpc_url", "KAGENT_GRPC_URL")
	viper.MustBindEnv("kagent_grpc_tls", "KAGENT_GRPC_TLS")
	viper.MustBindEnv("kagent_grpc_ca_file", "KAGENT_GRPC_CA_FILE")
	viper.MustBindEnv("kagent_grpc_server_name", "KAGENT_GRPC_SERVER_NAME")
	viper.MustBindEnv("USER_ID")

	if err := viper.ReadInConfig(); err != nil {
		// If config file doesn't exist, create it with defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); ok || os.IsNotExist(err) {
			if err := viper.WriteConfigAs(configFile); err != nil {
				return fmt.Errorf("error creating default config file: %w", err)
			}
		} else {
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
	return &config, nil
}
