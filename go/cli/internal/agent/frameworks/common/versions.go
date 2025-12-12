package common

// FrameworkVersions holds version information for framework dependencies
type FrameworkVersions struct {
	CrewAI     map[string]string
	LangGraph  map[string]string
	KagentCore string
}

// DefaultVersions returns the default versions for all frameworks
func DefaultVersions() *FrameworkVersions {
	return &FrameworkVersions{
		CrewAI: map[string]string{
			"crewai":       "^0.76.0",
			"crewai-tools": "^0.12.0",
		},
		LangGraph: map[string]string{
			"langgraph": "^0.2.16",
			"langchain": "^0.3.0",
		},
		KagentCore: "^0.3.0",
	}
}

// GetCrewAIVersion returns the CrewAI package version
func (v *FrameworkVersions) GetCrewAIVersion() string {
	return v.CrewAI["crewai"]
}

// GetCrewAIToolsVersion returns the CrewAI tools version
func (v *FrameworkVersions) GetCrewAIToolsVersion() string {
	return v.CrewAI["crewai-tools"]
}

// GetLangGraphVersion returns the LangGraph version
func (v *FrameworkVersions) GetLangGraphVersion() string {
	return v.LangGraph["langgraph"]
}

// GetLangChainVersion returns the LangChain version
func (v *FrameworkVersions) GetLangChainVersion() string {
	return v.LangGraph["langchain"]
}

// GetKagentCoreVersion returns the kagent-core version
func (v *FrameworkVersions) GetKagentCoreVersion() string {
	return v.KagentCore
}
