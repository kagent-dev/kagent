package image

import "fmt"

const (
	DefaultRegistry = "localhost:5001"
	DefaultTag      = "latest"
)

// ConstructImageName constructs a Docker image name with default registry and tag.
// If configImage is provided (non-empty), it is returned as-is.
// Otherwise, constructs: DefaultRegistry/imageName:DefaultTag (e.g., "localhost:5001/my-agent:latest")
func ConstructImageName(configImage, imageName, registry string) string {
    if configImage != "" {
        return configImage
    }

    if registry == "" {
        registry = DefaultRegistry
    }

    return fmt.Sprintf("%s/%s:%s", registry, imageName, DefaultTag)
}

// ConstructMCPServerImageName constructs a Docker image name for an MCP server.
// The image name follows the pattern: DefaultRegistry/agentName-serverName:DefaultTag
// (e.g., "localhost:5001/my-agent-github-server:latest")
func ConstructMCPServerImageName(agentName, serverName, registry string) string {
    if registry == "" {
        registry = DefaultRegistry
    }
    imageName := fmt.Sprintf("%s-%s", agentName, serverName)
    return fmt.Sprintf("%s/%s:%s", registry, imageName, DefaultTag)
}
