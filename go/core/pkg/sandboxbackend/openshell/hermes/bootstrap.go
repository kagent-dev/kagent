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

// BuildHermesEnvFile returns .env file bytes for Hermes bootstrap (gateway port, channel placeholders, allowlists).
// Resolved channel secrets belong in execEnv; callers populate that separately (see BuildBootstrapArtifacts).
func BuildHermesEnvFile(msg *messagingState) []byte {
	lines := []string{
		fmt.Sprintf("API_SERVER_PORT=%d", HermesInternalGatewayPort),
		"API_SERVER_HOST=127.0.0.1",
	}
	if msg != nil && msg.resolved != nil {
		for _, tg := range msg.resolved.Telegram {
			botKey := channels.TelegramBotTokenEnvKey(tg.Name)
			lines = append(lines, botKey+"="+channels.ResolveEnvPlaceholder(botKey))
			if len(tg.AllowFrom) > 0 {
				lines = append(lines, channels.TelegramAllowedUsersEnvKey(tg.Name)+"="+strings.Join(tg.AllowFrom, ","))
			}
		}
		for _, sl := range msg.resolved.Slack {
			botKey := channels.SlackBotTokenEnvKey(sl.Name)
			appKey := channels.SlackAppTokenEnvKey(sl.Name)
			lines = append(lines,
				botKey+"="+channels.SlackBotTokenPlaceholder(botKey),
				appKey+"="+channels.SlackAppTokenPlaceholder(appKey),
			)
			if len(sl.AllowedUserIDs) > 0 {
				lines = append(lines, channels.SlackAllowedUsersEnvKey(sl.Name)+"="+strings.Join(sl.AllowedUserIDs, ","))
			}
			if home := strings.TrimSpace(sl.HomeChannel); home != "" {
				lines = append(lines, channels.SlackHomeChannelEnvKey(sl.Name)+"="+home)
				if name := strings.TrimSpace(sl.HomeChannelName); name != "" {
					lines = append(lines, channels.SlackHomeChannelNameEnvKey(sl.Name)+"="+name)
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
		msg, err = AccumulateMessagingChannels(ctx, kube, namespace, ah.Spec.Backend, ah.Spec.Channels, nil)
		if err != nil {
			return nil, nil, nil, err
		}
		maps.Copy(execEnv, msg.secrets())
	}
	configYAML, err = BuildHermesConfigYAML(mc, msg)
	if err != nil {
		return nil, nil, nil, err
	}
	envFile = BuildHermesEnvFile(msg)
	return configYAML, envFile, execEnv, nil
}
