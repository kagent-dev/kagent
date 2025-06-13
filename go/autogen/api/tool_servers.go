package api

type ToolServerConfig struct {
	//ONEOF
	*StdioMcpServerConfig
	*SseMcpServerConfig
}

func (c *ToolServerConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *ToolServerConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type StdioMcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type SseMcpServerConfig struct {
	URL            string                 `json:"url"`
	Headers        map[string]interface{} `json:"headers,omitempty"`
	Timeout        int                    `json:"timeout,omitempty"`
	SseReadTimeout int                    `json:"sse_read_timeout,omitempty"`
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
