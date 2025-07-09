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
)

func main() {
    // Create a new client set
    c := client.NewClientSet("http://localhost:8080", 
        client.WithUserID("your-user-id"))

    // Check server health
    if err := c.Health().Health(context.Background()); err != nil {
        log.Fatal("Server is not healthy:", err)
    }

    // Get version information
    version, err := c.Version().GetVersion(context.Background())
    if err != nil {
        log.Fatal("Failed to get version:", err)
    }
    fmt.Printf("Server version: %s\n", version.KAgentVersion)
}
```

## Client Architecture

The client is organized into sub-clients, each responsible for a specific API area:

- **Health**: `c.Health()` - Server health checks
- **Version**: `c.Version()` - Version information
- **ModelConfigs**: `c.ModelConfigs()` - Model configuration management
- **Sessions**: `c.Sessions()` - Session management
- **Teams**: `c.Teams()` - Team management
- **Tools**: `c.Tools()` - Tool listing
- **ToolServers**: `c.ToolServers()` - Tool server management
- **Memories**: `c.Memories()` - Memory management
- **Providers**: `c.Providers()` - Provider information
- **Models**: `c.Models()` - Model information
- **Namespaces**: `c.Namespaces()` - Namespace listing
- **Feedback**: `c.Feedback()` - Feedback management

## Configuration

### Client Options

```go
// With custom HTTP client
httpClient := &http.Client{Timeout: 60 * time.Second}
c := client.NewClientSet("http://localhost:8080", 
    client.WithHTTPClient(httpClient),
    client.WithUserID("your-user-id"))
```

### User ID

Many endpoints require a user ID. You can either:

1. Set a default user ID when creating the client:
```go
c := client.NewClientSet("http://localhost:8080", client.WithUserID("user123"))
```

2. Pass it explicitly to methods that require it:
```go
sessions, err := c.Sessions().ListSessions(ctx, "user123")
```

## API Methods

### Health and Version

```go
// Health check
err := c.Health(ctx)

// Get version information
version, err := c.GetVersion(ctx)
```

### Model Configurations

```go
// List all model configurations
configs, err := c.ListModelConfigs(ctx)

// Get a specific model configuration
config, err := c.GetModelConfig(ctx, "namespace", "config-name")

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
config, err := c.CreateModelConfig(ctx, request)

// Update a model configuration
updateReq := &client.UpdateModelConfigRequest{
    Provider: client.Provider{Type: "OpenAI"},
    Model:    "gpt-4-turbo",
    OpenAIParams: &v1alpha1.OpenAIConfig{
        Temperature: "0.8",
    },
}
config, err := c.UpdateModelConfig(ctx, "namespace", "config-name", updateReq)

// Delete a model configuration
err := c.DeleteModelConfig(ctx, "namespace", "config-name")
```

### Sessions

```go
// List sessions for a user
sessions, err := c.ListSessions(ctx, "user123")

// Create a new session
sessionReq := &client.SessionRequest{
    Name:   "My Session",
    UserID: "user123",
    TeamID: &teamID, // optional
}
session, err := c.CreateSession(ctx, sessionReq)

// Get a specific session
session, err := c.GetSession(ctx, "session-name", "user123")

// Update a session
sessionReq.TeamID = &newTeamID
session, err := c.UpdateSession(ctx, sessionReq)

// Delete a session
err := c.DeleteSession(ctx, "session-name", "user123")

// List runs for a session
runs, err := c.ListSessionRuns(ctx, "session-name", "user123")
```

### Teams

```go
// List teams for a user
teams, err := c.ListTeams(ctx, "user123")

// Create a new team
teamReq := &client.TeamRequest{
    AgentRef: "default/my-agent",
    Component: api.Component{
        Label: "My Team",
        Description: "Team description",
    },
}
team, err := c.CreateTeam(ctx, teamReq)

// Get a specific team
team, err := c.GetTeam(ctx, "team-id")

// Update a team
team, err := c.UpdateTeam(ctx, "team-id", teamReq)

// Delete a team
err := c.DeleteTeam(ctx, "team-id")
```

### Tools

```go
// List tools for a user
tools, err := c.ListTools(ctx, "user123")
```

### Tool Servers

```go
// List all tool servers
toolServers, err := c.ListToolServers(ctx)

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
created, err := c.CreateToolServer(ctx, toolServer)

// Delete a tool server
err := c.DeleteToolServer(ctx, "namespace", "tool-server-name")
```

### Memories

```go
// List all memories
memories, err := c.ListMemories(ctx)

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
memory, err := c.CreateMemory(ctx, memoryReq)

// Get a specific memory
memory, err := c.GetMemory(ctx, "namespace", "memory-name")

// Update a memory
updateReq := &client.UpdateMemoryRequest{
    PineconeParams: &v1alpha1.PineconeConfig{
        IndexHost: "new-index.pinecone.io",
        TopK:      20,
    },
}
memory, err := c.UpdateMemory(ctx, "namespace", "memory-name", updateReq)

// Delete a memory
err := c.DeleteMemory(ctx, "namespace", "memory-name")
```

### Providers

```go
// List supported model providers
modelProviders, err := c.ListSupportedModelProviders(ctx)

// List supported memory providers
memoryProviders, err := c.ListSupportedMemoryProviders(ctx)
```

### Models

```go
// List supported models
models, err := c.ListSupportedModels(ctx)
```

### Namespaces

```go
// List namespaces
namespaces, err := c.ListNamespaces(ctx)
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
err := c.CreateFeedback(ctx, feedback, "user123")

// List feedback for a user
feedback, err := c.ListFeedback(ctx, "user123")
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

## Legacy Compatibility

For backward compatibility, you can still use the legacy `New()` function, though it returns the same clientset interface:

```go
// Legacy usage (still works)
c := client.New("http://localhost:8080", client.WithUserID("user123"))

// Alternative modern usage
c := client.NewClientSet("http://localhost:8080", client.WithUserID("user123"))
c := client.NewClient("http://localhost:8080", client.WithUserID("user123"))
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
    c := client.NewClient("http://localhost:8080", client.WithUserID("user123"))
    ctx := context.Background()

    // Create a session
    sessionReq := &client.SessionRequest{
        Name:   "Chat Session",
        UserID: "user123",
    }
    session, err := c.CreateSession(ctx, sessionReq)
    if err != nil {
        log.Fatal("Failed to create session:", err)
    }

    // List all sessions
    sessions, err := c.ListSessions(ctx, "user123")
    if err != nil {
        log.Fatal("Failed to list sessions:", err)
    }
    
    log.Printf("Created session: %+v\n", session)
    log.Printf("Total sessions: %d\n", len(sessions))

    // Get session runs
    runs, err := c.ListSessionRuns(ctx, session.Name, "user123")
    if err != nil {
        log.Fatal("Failed to get session runs:", err)
    }
    
    log.Printf("Session runs: %d\n", len(runs))

    // Clean up
    err = c.DeleteSession(ctx, session.Name, "user123")
    if err != nil {
        log.Fatal("Failed to delete session:", err)
    }
}
```