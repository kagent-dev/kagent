# KAgent Client

HTTP Client to interact with the KAgent API.

## Installation

```go
import "github.com/kagent-dev/kagent/go/pkg/client"
```

## Basic Usage

The client library provides a modular, interface-based design with sub-clients for different API areas:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/kagent-dev/kagent/go/pkg/client"
    "github.com/kagent-dev/kagent/go/pkg/client/api"
    "github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {
    // Create a new client set
    c := client.New("http://localhost:8080",
        client.WithUserID("your-user-id"))

    // Check server health
    if err := c.Health.Get(context.Background()); err != nil {
        log.Fatal("Server is not healthy:", err)
    }

    // Get version information
    version, err := c.Version.GetVersion(context.Background())
    if err != nil {
        log.Fatal("Failed to get version:", err)
    }
    fmt.Printf("Server version: %s\n", version.KAgentVersion)
}
```

## Client Architecture

The client is organized into sub-clients, each responsible for a specific API area:

- **Health**: `c.Health` - Server health checks
- **Version**: `c.Version` - Version information
- **ModelConfigs**: `c.ModelConfig` - Model configuration management
- **Sessions**: `c.Session` - Session management
- **Agents**: `c.Agent` - Agent management
- **Tools**: `c.Tool` - Tool listing
- **ToolServers**: `c.ToolServer` - Tool server management
- **Memories**: `c.Memory` - Memory management
- **Providers**: `c.Provider` - Provider information
- **Models**: `c.Model` - Model information
- **Namespaces**: `c.Namespace` - Namespace listing
- **Feedback**: `c.Feedback` - Feedback management

## Configuration

### Client Options

```go
// With custom HTTP client
httpClient := &http.Client{Timeout: 60 * time.Second}
c := client.New("http://localhost:8080",
    client.WithHTTPClient(httpClient),
    client.WithUserID("your-user-id"))
```

### User ID

Many endpoints require a user ID. You can either:

1. Set a default user ID when creating the client:
```go
c := client.New("http://localhost:8080", client.WithUserID("user123"))
```

2. Pass it explicitly to methods that require it:
```go
sessions, err := c.Sessions().ListSessions(ctx, "user123")
```

## API Methods

### Health and Version

```go
// Health check
err := c.Health.Get(ctx)

// Get version information
version, err := c.Version.GetVersion(ctx)
```

### Model Configurations

```go
// List all model configurations
configs, err := c.ModelConfig.ListModelConfigs(ctx)

// Get a specific model configuration
config, err := c.ModelConfig.GetModelConfig(ctx, "namespace", "config-name")

// Create a new model configuration
request := &client.CreateModelConfigRequest{
    Ref:      "default/my-config",
    Provider: client.Provider{Type: "OpenAI"},
    Model:    "gpt-4",
    APIKey:   "your-api-key",
    OpenAIParams: &v1alpha1.OpenAIConfig{
        Temperature: "0.7",
        MaxTokens:   1000,
    },
}
config, err := c.ModelConfig.CreateModelConfig(ctx, request)

// Update a model configuration
updateReq := &client.UpdateModelConfigRequest{
    Provider: client.Provider{Type: "OpenAI"},
    Model:    "gpt-4-turbo",
    OpenAIParams: &v1alpha1.OpenAIConfig{
        Temperature: "0.8",
    },
}
config, err := c.ModelConfig.UpdateModelConfig(ctx, "namespace", "config-name", updateReq)

// Delete a model configuration
err := c.ModelConfig.DeleteModelConfig(ctx, "namespace", "config-name")
```

### Sessions

```go
// List sessions for a user
sessions, err := c.Session.ListSessions(ctx, "user123")

// Create a new session
sessionReq := &client.SessionRequest{
    Name:     "My Session",
    UserID:   "user123",
    AgentRef: &agentRef, // optional
}
session, err := c.Session.CreateSession(ctx, sessionReq)

// Get a specific session
session, err := c.Session.GetSession(ctx, "session-name", "user123")

// Update a session
sessionReq.AgentRef = &newAgentRef
session, err := c.Session.UpdateSession(ctx, sessionReq)

// Delete a session
err := c.Session.DeleteSession(ctx, "session-name", "user123")

// List runs for a session
runs, err := c.Session.ListSessionRuns(ctx, "session-name", "user123")
```

### Agents

```go
// List agents for a user
agents, err := c.Agent.ListAgents(ctx, "user123")

// Create a new agent
agent := &v1alpha1.Agent{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "my-agent",
        Namespace: "default",
    },
    Spec: v1alpha1.AgentSpec{
        Description:   "My agent description",
        SystemMessage: "You are a helpful assistant",
        ModelConfig:   "default/gpt-4-config",
    },
}
createdAgent, err := c.Agent.CreateAgent(ctx, agent)

// Get a specific agent
agentResponse, err := c.Agent.GetAgent(ctx, "default/my-agent")

// Update an agent
agent.Spec.Description = "Updated description"
updatedAgent, err := c.Agent.UpdateAgent(ctx, agent)

// Delete an agent
err := c.Agent.DeleteAgent(ctx, "default/my-agent")
```

### Tools

```go
// List tools for a user
tools, err := c.Tool.ListTools(ctx, "user123")
```

### Tool Servers

```go
// List all tool servers
toolServers, err := c.ToolServer.ListToolServers(ctx)

// Create a new tool server
toolServer := &v1alpha1.ToolServer{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "my-tool-server",
        Namespace: "default",
    },
    Spec: v1alpha1.ToolServerSpec{
        Description: "My tool server",
        Config: v1alpha1.ToolServerConfig{
            Stdio: &v1alpha1.StdioMcpServerConfig{
                Command: "python",
                Args:    []string{"-m", "my_tool"},
            },
        },
    },
}
created, err := c.ToolServer.CreateToolServer(ctx, toolServer)

// Delete a tool server
err := c.ToolServer.DeleteToolServer(ctx, "namespace", "tool-server-name")
```

### Memories

```go
// List all memories
memories, err := c.Memory.ListMemories(ctx)

// Create a new memory
memoryReq := &client.CreateMemoryRequest{
    Ref:      "default/my-memory",
    Provider: client.Provider{Type: "Pinecone"},
    APIKey:   "your-pinecone-api-key",
    PineconeParams: &v1alpha1.PineconeConfig{
        IndexHost: "my-index.pinecone.io",
        TopK:      10,
    },
}
memory, err := c.Memory.CreateMemory(ctx, memoryReq)

// Get a specific memory
memory, err := c.Memory.GetMemory(ctx, "namespace", "memory-name")

// Update a memory
updateReq := &client.UpdateMemoryRequest{
    PineconeParams: &v1alpha1.PineconeConfig{
        IndexHost: "new-index.pinecone.io",
        TopK:      20,
    },
}
memory, err := c.Memory.UpdateMemory(ctx, "namespace", "memory-name", updateReq)

// Delete a memory
err := c.Memory.DeleteMemory(ctx, "namespace", "memory-name")
```

### Providers

```go
// List supported model providers
modelProviders, err := c.Provider.ListSupportedModelProviders(ctx)

// List supported memory providers
memoryProviders, err := c.Provider.ListSupportedMemoryProviders(ctx)
```

### Models

```go
// List supported models
models, err := c.Model.ListSupportedModels(ctx)
```

### Namespaces

```go
// List namespaces
namespaces, err := c.Namespace.ListNamespaces(ctx)
```

### Feedback

```go
// Create feedback
feedback := &client.Feedback{
    MessageID:    123,
    IsPositive:   true,
    FeedbackText: "Great response!",
    IssueType:    nil, // optional
}
err := c.Feedback.CreateFeedback(ctx, feedback, "user123")

// List feedback for a user
feedback, err := c.Feedback.ListFeedback(ctx, "user123")
```

## Error Handling

The client returns structured errors that implement the error interface:

```go
configs, err := c.ListModelConfigs(ctx)
if err != nil {
    if clientErr, ok := err.(*client.ClientError); ok {
        fmt.Printf("HTTP %d: %s\n", clientErr.StatusCode, clientErr.Message)
        fmt.Printf("Response body: %s\n", clientErr.Body)
    } else {
        fmt.Printf("Client error: %v\n", err)
    }
}
```

## Client Constructor

The client is created using the `New()` function:

```go
// Standard usage
c := client.New("http://localhost:8080", client.WithUserID("user123"))
```

## Examples

### Complete Session Management

```go
package main

import (
    "context"
    "log"

    "github.com/kagent-dev/kagent/go/pkg/client"
)

func main() {
    c := client.New("http://localhost:8080", client.WithUserID("user123"))
    ctx := context.Background()

    // Create a session
    sessionReq := &client.SessionRequest{
        Name:   "Chat Session",
        UserID: "user123",
    }
    session, err := c.Session.CreateSession(ctx, sessionReq)
    if err != nil {
        log.Fatal("Failed to create session:", err)
    }

    // List all sessions
    sessions, err := c.Session.ListSessions(ctx, "user123")
    if err != nil {
        log.Fatal("Failed to list sessions:", err)
    }
    
    log.Printf("Created session: %+v\n", session)
    log.Printf("Total sessions: %d\n", len(sessions))

    // Get session runs
    runs, err := c.Session.ListSessionRuns(ctx, session.Name, "user123")
    if err != nil {
        log.Fatal("Failed to get session runs:", err)
    }
    
    log.Printf("Session runs: %d\n", len(runs))

    // Clean up
    err = c.Session.DeleteSession(ctx, session.Name, "user123")
    if err != nil {
        log.Fatal("Failed to delete session:", err)
    }
}
```