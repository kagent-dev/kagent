package api

type TeamToolConfig struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Team        *Component `json:"team"`
}

func (c *TeamToolConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *TeamToolConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}
