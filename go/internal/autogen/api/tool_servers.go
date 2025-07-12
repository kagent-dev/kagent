package api

type StdioMcpServerConfig struct {
	Command            string            `json:"command"`
	Args               []string          `json:"args,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	ReadTimeoutSeconds uint8             `json:"read_timeout_seconds,omitempty"`
}

func (c *StdioMcpServerConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *StdioMcpServerConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type SseMcpServerConfig struct {
	URL            string                 `json:"url"`
	Headers        map[string]interface{} `json:"headers,omitempty"`
	Timeout        *float64               `json:"timeout,omitempty"`
	SseReadTimeout *float64               `json:"sse_read_timeout,omitempty"`
}

func (c *SseMcpServerConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *SseMcpServerConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type StreamableHttpServerConfig struct {
	URL              string                 `json:"url"`
	Headers          map[string]interface{} `json:"headers,omitempty"`
	Timeout          *float64               `json:"timeout,omitempty"`
	SseReadTimeout   *float64               `json:"sse_read_timeout,omitempty"`
	TerminateOnClose bool                   `json:"terminate_on_close,omitempty"`
}

func (c *StreamableHttpServerConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *StreamableHttpServerConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type MCPToolConfig struct {
	// can be StdioMcpServerConfig | SseMcpServerConfig
	ServerParams any     `json:"server_params"`
	Tool         MCPTool `json:"tool"`
}

type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}
