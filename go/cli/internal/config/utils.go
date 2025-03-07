package config

import (
	"github.com/abiosoft/ishell/v2"
	"github.com/fatih/color"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

const (
	configKey = "[config]"
	clientKey = "[client]"
)

func SetCfg(shell *ishell.Shell, cfg *Config) {
	shell.Set(configKey, cfg)
}

func SetClient(shell *ishell.Shell, client *autogen_client.Client) {
	shell.Set(clientKey, client)
}

func GetCfg(shell *ishell.Context) *Config {
	return shell.Get(configKey).(*Config)
}

func GetClient(shell *ishell.Context) *autogen_client.Client {
	return shell.Get(clientKey).(*autogen_client.Client)
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
