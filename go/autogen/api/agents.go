package api

type Handoff struct {
	Target      string `json:"target"`
	Description string `json:"description"`
	Name        string `json:"name"`
	Message     string `json:"message"`
}

type AssistantAgentConfig struct {
	Name                  string       `json:"name"`
	Description           string       `json:"description"`
	ModelClient           *Component   `json:"model_client,omitempty"`
	Tools                 []*Component `json:"tools"`
	ModelContext          *Component   `json:"model_context,omitempty"`
	SystemMessage         string       `json:"system_message,omitempty"`
	ReflectOnToolUse      bool         `json:"reflect_on_tool_use"`
	ModelClientStream     bool         `json:"model_client_stream"`
	ToolCallSummaryFormat string       `json:"tool_call_summary_format,omitempty"`
	Handoffs              []Handoff    `json:"handoffs,omitempty"`
}

func (c *AssistantAgentConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *AssistantAgentConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type MultiModalWebSurferConfig struct {
	Name              string     `json:"name"`
	ModelClient       *Component `json:"model_client,omitempty"`
	DownloadsFolder   *string    `json:"downloads_folder,omitempty"`
	Description       string     `json:"description"`
	DebugDir          *string    `json:"debug_dir,omitempty"`
	Headless          *bool      `json:"headless,omitempty"`
	StartPage         *string    `json:"start_page,omitempty"`
	AnimateActions    *bool      `json:"animate_actions,omitempty"`
	ToSaveScreenshots *bool      `json:"to_save_screenshots,omitempty"`
	UseOCR            *bool      `json:"use_ocr,omitempty"`
	BrowserChannel    *string    `json:"browser_channel,omitempty"`
	BrowserDataDir    *string    `json:"browser_data_dir,omitempty"`
	ToResizeViewport  *bool      `json:"to_resize_viewport,omitempty"`
}

func (c *MultiModalWebSurferConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *MultiModalWebSurferConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}

type TaskAgentConfig struct {
	Name         string     `json:"name"`
	Team         *Component `json:"team,omitempty"`
	ModelContext *Component `json:"model_context,omitempty"`
	Description  *string    `json:"description,omitempty"`
}

func (c *TaskAgentConfig) ToConfig() (map[string]interface{}, error) {
	return toConfig(c)
}

func (c *TaskAgentConfig) FromConfig(config map[string]interface{}) error {
	return fromConfig(c, config)
}
