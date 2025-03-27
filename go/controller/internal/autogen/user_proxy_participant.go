package autogen

import "github.com/kagent-dev/kagent/go/autogen/api"

// gets appended to all group chats
var userProxyAgent = &api.Component{
	Provider:      "autogen_agentchat.agents.UserProxyAgent",
	ComponentType: "agent",
	Version:       1,
	Description:   "An agent that can represent a human user through an input function.",
	Config: api.MustToConfig(&api.AssistantAgentConfig{
		Name:        "user_proxy",
		Description: "A human user",
	}),
	Label: "UserProxyAgent",
}
