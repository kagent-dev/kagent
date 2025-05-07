package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	APIURL    string `mapstructure:"api_url"`
	WSURL     string `mapstructure:"ws_url"`
	UserID    string `mapstructure:"user_id"`
	Namespace string `mapstructure:"namespace"`
	A2AURL    string `mapstructure:"a2a_url"`
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
	viper.SetDefault("api_url", "http://localhost:8081/api")
	viper.SetDefault("ws_url", "ws://localhost:8081/api/ws")
	viper.SetDefault("user_id", "admin@kagent.dev")
	viper.SetDefault("output_format", "table")
	viper.SetDefault("namespace", "kagent")
	viper.SetDefault("a2a_url", "http://localhost:8083/api/a2a")

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
