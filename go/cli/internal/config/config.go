package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	KAgentURL    string `mapstructure:"kagent_url"`
	Namespace    string `mapstructure:"namespace"`
	OutputFormat string `mapstructure:"output_format"`
	Verbose      bool   `mapstructure:"verbose"`
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
	viper.SetDefault("output_format", "table")
	viper.SetDefault("namespace", "kagent")

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
