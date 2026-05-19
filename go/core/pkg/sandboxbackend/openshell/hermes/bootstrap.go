package hermes

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/channels"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// hermesConfig is the YAML shape written to ~/.hermes/config.yaml.
type hermesConfig struct {
	ConfigVersion int             `yaml:"_config_version"`
	Model         hermesModel     `yaml:"model"`
	Terminal      hermesTerminal  `yaml:"terminal"`
	Agent         hermesAgent     `yaml:"agent"`
	Memory        hermesMemory    `yaml:"memory"`
	Skills        hermesSkills    `yaml:"skills"`
	Display       hermesDisplay   `yaml:"display"`
	Platforms     hermesPlatforms `yaml:"platforms"`
	Telegram      *hermesTelegram `yaml:"telegram,omitempty"`
}

type hermesModel struct {
	Default  string `yaml:"default"`
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
}

type hermesTerminal struct {
	Backend string `yaml:"backend"`
	Timeout int    `yaml:"timeout"`
}

type hermesAgent struct {
	MaxTurns        int    `yaml:"max_turns"`
	ReasoningEffort string `yaml:"reasoning_effort"`
}

type hermesMemory struct {
	MemoryEnabled      bool `yaml:"memory_enabled"`
	UserProfileEnabled bool `yaml:"user_profile_enabled"`
}

type hermesSkills struct {
	CreationNudgeInterval int `yaml:"creation_nudge_interval"`
}

type hermesDisplay struct {
	Compact      bool   `yaml:"compact"`
	ToolProgress string `yaml:"tool_progress"`
}

type hermesPlatforms struct {
	APIServer hermesAPIServer `yaml:"api_server"`
}

type hermesAPIServer struct {
	Enabled bool              `yaml:"enabled"`
	Extra   hermesAPIServerEx `yaml:"extra"`
}

type hermesAPIServerEx struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

type hermesTelegram struct {
	RequireMention bool `yaml:"require_mention"`
}

// BuildHermesConfigYAML returns config.yaml bytes for the given ModelConfig.
func BuildHermesConfigYAML(mc *v1alpha2.ModelConfig, msg *messagingState) ([]byte, error) {
	if mc == nil {
		return nil, fmt.Errorf("ModelConfig is required")
	}
	modelID := strings.TrimSpace(mc.Spec.Model)
	if modelID == "" {
		return nil, fmt.Errorf("ModelConfig.spec.model is required for Hermes bootstrap")
	}

	cfg := hermesConfig{
		ConfigVersion: 12,
		Model: hermesModel{
			Default:  modelID,
			Provider: "custom",
			BaseURL:  DefaultInferenceBaseURL,
		},
		Terminal: hermesTerminal{Backend: "local", Timeout: 180},
		Agent:    hermesAgent{MaxTurns: 60, ReasoningEffort: "medium"},
		Memory: hermesMemory{
			MemoryEnabled:      true,
			UserProfileEnabled: true,
		},
		Skills:  hermesSkills{CreationNudgeInterval: 15},
		Display: hermesDisplay{Compact: false, ToolProgress: "all"},
		Platforms: hermesPlatforms{
			APIServer: hermesAPIServer{
				Enabled: true,
				Extra: hermesAPIServerEx{
					Port: HermesInternalGatewayPort,
					Host: "127.0.0.1",
				},
			},
		},
	}
	if msg != nil && msg.hasTelegram() {
		cfg.Telegram = &hermesTelegram{RequireMention: true}
	}

	raw, err := yaml.Marshal(&cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal hermes config yaml: %w", err)
	}
	return raw, nil
}

// BuildHermesEnvFile returns .env file bytes and populates execEnv with resolved channel secrets.
func BuildHermesEnvFile(msg *messagingState, execEnv map[string]string) []byte {
	lines := []string{
		fmt.Sprintf("API_SERVER_PORT=%d", HermesInternalGatewayPort),
		"API_SERVER_HOST=127.0.0.1",
	}
	if msg != nil {
		if msg.hasTelegram() {
			lines = append(lines, "TELEGRAM_BOT_TOKEN="+channels.ResolveEnvPlaceholder(channels.EnvTelegramBotToken))
			if allow := msg.telegramAllow(); len(allow) > 0 {
				lines = append(lines, "TELEGRAM_ALLOWED_USERS="+strings.Join(allow, ","))
			}
		}
		if msg.hasSlack() {
			lines = append(lines,
				"SLACK_BOT_TOKEN="+channels.SlackBotTokenPlaceholder(),
				"SLACK_APP_TOKEN="+channels.SlackAppTokenPlaceholder(),
			)
			if allow := msg.slackAllow(); len(allow) > 0 {
				lines = append(lines, channels.EnvSlackAllowedUsers+"="+strings.Join(allow, ","))
			}
			if home := msg.slackHomeChannel(); home != "" {
				lines = append(lines, channels.EnvSlackHomeChannel+"="+home)
				if name := msg.slackHomeChannelName(); name != "" {
					lines = append(lines, channels.EnvSlackHomeChannelName+"="+name)
				}
			}
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// BuildBootstrapArtifacts builds config.yaml, .env, and exec environment for Hermes bootstrap.
func BuildBootstrapArtifacts(ctx context.Context, kube client.Client, namespace string, ah *v1alpha2.AgentHarness, mc *v1alpha2.ModelConfig) (configYAML, envFile []byte, execEnv map[string]string, err error) {
	execEnv = map[string]string{}
	var msg *messagingState
	if ah != nil && len(ah.Spec.Channels) > 0 {
		msg, err = AccumulateMessagingChannels(ctx, kube, namespace, ah.Spec.Channels, nil)
		if err != nil {
			return nil, nil, nil, err
		}
		maps.Copy(execEnv, msg.secrets())
	}
	configYAML, err = BuildHermesConfigYAML(mc, msg)
	if err != nil {
		return nil, nil, nil, err
	}
	envFile = BuildHermesEnvFile(msg, execEnv)
	return configYAML, envFile, execEnv, nil
}
