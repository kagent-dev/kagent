package config

import (
	"fmt"
	"os"
	"path"

	"github.com/abiosoft/ishell/v2"
	"github.com/fatih/color"
	"github.com/kagent-dev/kagent/go/pkg/client"
)

const (
	configKey = "[config]"
	clientKey = "[client]"
)

func SetCfg(shell *ishell.Shell, cfg *Config) {
	shell.Set(configKey, cfg)
}

func SetClient(shell *ishell.Shell, client *client.ClientSet) {
	shell.Set(clientKey, client)
}

func GetCfg(shell *ishell.Context) *Config {
	return shell.Get(configKey).(*Config)
}

func GetClient(shell *ishell.Context) *client.ClientSet {
	return shell.Get(clientKey).(*client.ClientSet)
}

func BoldBlue(s string) string {
	return color.New(color.FgBlue, color.Bold).SprintFunc()(s)
}

func BoldGreen(s string) string {
	return color.New(color.FgGreen, color.Bold).SprintFunc()(s)
}

func BoldYellow(s string) string {
	return color.New(color.FgYellow, color.Bold).SprintFunc()(s)
}

func BoldRed(s string) string {
	return color.New(color.FgRed, color.Bold).SprintFunc()(s)
}

func GetConfigDir(homeDir string) (string, error) {
	if homeDir == "" {
		return "", fmt.Errorf("homeDir cannot be empty")
	}

	if _, err := os.Stat(homeDir); os.IsNotExist(err) {
		return "", fmt.Errorf("homeDir should be a valid directory")
	}

	configDir := path.Join(homeDir, ".config", "kagent")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("error creating config directory: %w", err)
	}
	return configDir, nil
}

func SetHistoryPath(homeDir string, shell *ishell.Shell) {
	configDir, err := GetConfigDir(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting config directory: %v\n", err)
		return
	}
	historyPath := path.Join(configDir, ".kagent_history")
	shell.SetHistoryPath(historyPath)
}
