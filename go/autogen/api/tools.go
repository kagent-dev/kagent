package api

// HTTPToolConfig represents the configuration for HTTP tools
type HTTPToolConfig struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Scheme      string                 `json:"scheme"`
	Host        string                 `json:"host"`
	Port        int                    `json:"port"`
	Path        string                 `json:"path"`
	Method      string                 `json:"method"`
	Headers     map[string]string      `json:"headers"`
	JSONSchema  map[string]interface{} `json:"json_schema"`
}

func (c *HTTPToolConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *HTTPToolConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type GenericToolConfig map[string]interface{}

func (c *GenericToolConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *GenericToolConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

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
