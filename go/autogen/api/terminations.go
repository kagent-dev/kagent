package api

type OrTerminationConfig struct {
	Conditions []*Component `json:"conditions"`
}

func (c *OrTerminationConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *OrTerminationConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type AndTerminationConfig struct {
	Conditions []*Component `json:"conditions"`
}

func (c *AndTerminationConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *AndTerminationConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type TextMentionTerminationConfig struct {
	Text *string `json:"text"`
}

func (c *TextMentionTerminationConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *TextMentionTerminationConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type TextMessageTerminationConfig struct {
	Source *string `json:"source"`
}

func (c *TextMessageTerminationConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *TextMessageTerminationConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type MaxMessageTerminationConfig struct {
	MaxMessages *int `json:"max_messages"`
}

func (c *MaxMessageTerminationConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *MaxMessageTerminationConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type StopMessageTerminationConfig struct{}

func (c *StopMessageTerminationConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *StopMessageTerminationConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}
