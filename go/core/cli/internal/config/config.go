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
	KAgentURL    string        `mapstructure:"kagent_url"`
	Namespace    string        `mapstructure:"namespace"`
	OutputFormat string        `mapstructure:"output_format"`
	Verbose      bool          `mapstructure:"verbose"`
	Timeout      time.Duration `mapstructure:"timeout"`
}

func (c *Config) Client() *kagentclient.ClientSet {
	return kagentclient.New(c.KAgentURL, kagentclient.WithUserID("admin@kagent.dev"))
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
	viper.SetDefault("timeout", 300*time.Second)
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

// BindFlags binds cobra persistent flags to viper keys so that config file
// values are used as defaults when the corresponding CLI flag is not explicitly
// set. Flag names use dashes (e.g. "kagent-url") while config file keys use
// underscores (e.g. "kagent_url"), so each binding is explicit.
func BindFlags(flags *pflag.FlagSet) {
	viper.BindPFlag("kagent_url", flags.Lookup("kagent-url"))     //nolint:errcheck
	viper.BindPFlag("namespace", flags.Lookup("namespace"))        //nolint:errcheck
	viper.BindPFlag("output_format", flags.Lookup("output-format")) //nolint:errcheck
	viper.BindPFlag("verbose", flags.Lookup("verbose"))            //nolint:errcheck
	viper.BindPFlag("timeout", flags.Lookup("timeout"))            //nolint:errcheck
}

// Apply populates the given Config from the merged viper state (config file +
// env + CLI flags). Call this after flag parsing to ensure cfg reflects all
// sources with the correct precedence: CLI flag > env > config file > default.
func Apply(cfg *Config) error {
	merged, err := Get()
	if err != nil {
		return err
	}
	*cfg = *merged
	return nil
}

func Get() (*Config, error) {
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}
	return &config, nil
}
