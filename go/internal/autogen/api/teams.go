package api

type CommonTeamConfig struct {
	Participants []*Component `json:"participants"`
	Termination  *Component   `json:"termination_condition,omitempty"`
	MaxTurns     *int         `json:"max_turns,omitempty"`
	ModelConfig  *Component   `json:"model_config,omitempty"`
}

func (c *CommonTeamConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *CommonTeamConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type RoundRobinGroupChatConfig struct {
	CommonTeamConfig
}

func (c *RoundRobinGroupChatConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *RoundRobinGroupChatConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type SelectorGroupChatConfig struct {
	CommonTeamConfig
	SelectorPrompt string `json:"selector_prompt,omitempty"`
}

func (c *SelectorGroupChatConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *SelectorGroupChatConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type MagenticOneGroupChatConfig struct {
	CommonTeamConfig
	FinalAnswerPrompt string `json:"final_answer_prompt,omitempty"`
	MaxStalls         int    `json:"max_stalls,omitempty"`
}

func (c *MagenticOneGroupChatConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *MagenticOneGroupChatConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type SwarmTeamConfig struct {
	CommonTeamConfig
}

func (c *SwarmTeamConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *SwarmTeamConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}
