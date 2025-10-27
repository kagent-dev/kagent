# A2A Protocol Implementation

## Overview

The `go/internal/a2a` package implements the Agent-to-Agent (A2A) protocol for Kagent. It provides HTTP routing, task management, and protocol utilities for agent communication. The A2A protocol enables standardized communication between agents, clients, and the Kagent controller.

## Architecture

The package consists of three main components:
1. **A2A Handler Mux**: HTTP routing for agent endpoints
2. **Passthrough Manager**: Task management delegation to agent backends
3. **Protocol Utilities**: Helper functions for A2A message handling

## Key Components

### A2AHandlerMux Interface (`a2a_handler_mux.go`)

#### Purpose
Manages HTTP handlers for multiple agents dynamically, routing A2A protocol requests to the appropriate agent backend.

#### Interface Definition (lines 17-27)
```go
type A2AHandlerMux interface {
    SetAgentHandler(agentRef string, client *client.A2AClient, card server.AgentCard) error
    RemoveAgentHandler(agentRef string)
    http.Handler
}
```

#### Implementation: handlerMux

##### Structure (lines 29-34)
- **handlers**: Map of agent reference → HTTP handler
- **lock**: RWMutex for thread-safe handler management
- **basePathPrefix**: URL path prefix for routing
- **authenticator**: Authentication provider for request validation

##### Key Methods

###### SetAgentHandler (lines 46-62)
- **Purpose**: Register A2A handler for an agent
- **Parameters**:
  - `agentRef`: Agent identifier (namespace/name)
  - `client`: A2A client for agent communication
  - `card`: Agent card with metadata
- **Process**:
  1. Create A2A server with agent card
  2. Wrap with PassthroughManager for task delegation
  3. Add authentication middleware
  4. Store handler in map
- **Thread Safety**: Uses write lock

###### RemoveAgentHandler (lines 64-70)
- **Purpose**: Unregister agent handler
- **Process**: Delete handler from map
- **Thread Safety**: Uses write lock

###### ServeHTTP (lines 79-100+)
- **Purpose**: Route HTTP requests to appropriate agent handler
- **Process**:
  1. Extract namespace and name from URL path variables
  2. Construct agent reference (namespace/name)
  3. Lookup handler for agent
  4. Delegate request to agent's A2A server
  5. Return 404 if agent not found
- **URL Pattern**: `{pathPrefix}/{namespace}/{name}/*`
- **Thread Safety**: Uses read lock for handler lookup

##### Factory Function

###### NewA2AHttpMux (lines 38-44)
- **Purpose**: Create new handler multiplexer
- **Parameters**:
  - `pathPrefix`: Base URL path for A2A endpoints
  - `authenticator`: Auth provider for request validation
- **Returns**: Initialized handlerMux

### PassthroughManager (`manager.go`)

#### Purpose
Implements the `taskmanager.TaskManager` interface by delegating all operations to an A2A client. This creates a passthrough layer that forwards agent requests to the actual agent backend.

#### Structure (lines 11-18)
```go
type PassthroughManager struct {
    client *client.A2AClient
}
```

#### Key Methods

##### OnSendMessage (lines 21-29)
- **Purpose**: Send synchronous message to agent
- **Process**:
  1. Generate message ID if not provided
  2. Set message kind to "message" if not specified
  3. Delegate to A2A client's SendMessage
- **Returns**: MessageResult with response

##### OnSendMessageStream (lines 31-39)
- **Purpose**: Stream message to agent (real-time responses)
- **Process**:
  1. Generate message ID if not provided
  2. Set message kind if not specified
  3. Delegate to A2A client's StreamMessage
- **Returns**: Channel of streaming message events
- **Use Case**: Interactive chat with progressive responses

##### OnGetTask (lines 41-43)
- **Purpose**: Query task status and history
- **Delegates**: To A2A client's GetTasks

##### OnCancelTask (lines 45-47)
- **Purpose**: Cancel running task
- **Delegates**: To A2A client's CancelTasks

##### OnPushNotificationSet (lines 49-51)
- **Purpose**: Configure push notifications for task updates
- **Delegates**: To A2A client's SetPushNotification

##### OnPushNotificationGet (lines 53-55)
- **Purpose**: Retrieve push notification configuration
- **Delegates**: To A2A client's GetPushNotification

##### OnResubscribe (lines 57-59)
- **Purpose**: Resubscribe to task event stream (reconnect)
- **Delegates**: To A2A client's ResubscribeTask
- **Use Case**: Resume streaming after disconnection

##### Factory Function

###### NewPassthroughManager (lines 15-19)
- **Purpose**: Create new passthrough task manager
- **Parameters**: A2A client instance
- **Returns**: TaskManager interface

### Protocol Utilities (`a2a_utils.go`)

#### ExtractText Function (lines 10-18)
- **Purpose**: Extract text content from A2A message
- **Process**:
  1. Iterate through message parts
  2. Identify TextPart instances
  3. Concatenate text content
- **Returns**: Combined text string
- **Use Case**: Get plain text from multi-part messages

## A2A Protocol Flow

### Agent Registration
1. Controller creates A2A client for agent backend
2. Controller calls `SetAgentHandler(agentRef, client, agentCard)`
3. Handler mux creates A2A server with PassthroughManager
4. Agent becomes accessible via `/a2a/{namespace}/{name}/*`

### Message Flow (Synchronous)
1. Client sends POST to `/a2a/{namespace}/{name}/send-message`
2. handlerMux routes to agent's A2A server
3. A2A server calls PassthroughManager.OnSendMessage
4. PassthroughManager forwards to agent backend via A2A client
5. Agent processes message and returns response
6. Response flows back through chain to client

### Message Flow (Streaming)
1. Client sends POST to `/a2a/{namespace}/{name}/stream-message`
2. handlerMux routes to agent's A2A server
3. A2A server calls PassthroughManager.OnSendMessageStream
4. PassthroughManager opens stream to agent backend
5. Agent sends progressive updates via event channel
6. Client receives real-time streaming events

### Task Query Flow
1. Client sends GET to `/a2a/{namespace}/{name}/tasks/{taskId}`
2. handlerMux routes to agent handler
3. PassthroughManager.OnGetTask queries backend
4. Returns task status, messages, and metadata

### Agent Deregistration
1. Controller calls `RemoveAgentHandler(agentRef)`
2. Handler removed from routing map
3. Subsequent requests return 404

## Integration Points

### Controller Integration
The A2A handler mux is instantiated and managed by the HTTP server component (`go/internal/httpserver/`). When agents are deployed or removed, the controller updates the handler mux accordingly.

### Authentication
All A2A requests pass through authentication middleware:
- **Provider**: `auth.AuthProvider` interface
- **Implementation**: `authimpl.NewA2AAuthenticator`
- **Validation**: Request headers, tokens, user identity

### Agent Backends
PassthroughManager connects to agent backends running:
- **ADK Agents**: Python ADK runtime
- **LangGraph Agents**: Python LangGraph runtime
- **CrewAI Agents**: Python CrewAI runtime
- **Custom Agents**: Any A2A-compatible implementation

## Dependencies

### External Libraries
- **trpc-a2a-go**: A2A protocol SDK
  - `client`: A2A client for agent communication
  - `server`: A2A server for protocol endpoints
  - `protocol`: Protocol types and utilities
  - `taskmanager`: Task manager interface
- **gorilla/mux**: HTTP routing (for path variables)

### Internal Dependencies
- **go/internal/httpserver/auth**: Authentication implementation
- **go/internal/utils**: Common utilities (ResourceRefString)
- **go/pkg/auth**: Authentication provider interface

## Thread Safety

The handler mux uses sync.RWMutex for concurrent access:
- **Read Lock**: Used during request routing (high frequency)
- **Write Lock**: Used during agent registration/removal (low frequency)
- **Benefit**: Multiple concurrent requests can route while preventing race conditions during handler updates

## Error Handling

### Common Errors
- **404 Not Found**: Agent not registered or removed
- **400 Bad Request**: Missing namespace or name in URL
- **401 Unauthorized**: Authentication failure
- **500 Internal Server Error**: A2A server creation failure

### Message Validation
- **Message ID**: Auto-generated if missing (lines 22-24, 32-34)
- **Message Kind**: Defaults to "message" if not specified (lines 25-27, 35-37)

## URL Routing

### Path Pattern
```
/a2a/{namespace}/{name}/*
```

### Example Endpoints
- `/a2a/kagent/k8s-agent/send-message`: Synchronous message
- `/a2a/kagent/k8s-agent/stream-message`: Streaming message
- `/a2a/kagent/k8s-agent/tasks/{taskId}`: Task query
- `/a2a/kagent/k8s-agent/tasks/{taskId}/cancel`: Cancel task

### Path Variables
- **namespace**: Kubernetes namespace of agent
- **name**: Agent name
- Combined as `namespace/name` for handler lookup (line 94)

## Usage Example

```go
// Create handler mux
authProvider := auth.NewAuthProvider()
mux := a2a.NewA2AHttpMux("/a2a", authProvider)

// Register agent
a2aClient := client.NewA2AClient("http://agent-backend:8000")
agentCard := server.AgentCard{
    Name: "my-agent",
    Description: "Example agent",
}
err := mux.SetAgentHandler("kagent/my-agent", a2aClient, agentCard)

// Use as HTTP handler
http.Handle("/a2a/", mux)

// Remove agent
mux.RemoveAgentHandler("kagent/my-agent")
```

## Related Components

- **HTTP Server** (`go/internal/httpserver/`): Hosts the A2A handler mux
- **Controller** (`go/internal/controller/`): Manages agent lifecycle and registration
- **Agent Runtimes** (`python/packages/`): Backend implementations of agents
- **CLI** (`go/cli/`): Client for invoking agents via A2A
- **UI** (`ui/`): Web interface using A2A protocol

## Key Features

1. **Dynamic Routing**: Add/remove agent handlers at runtime
2. **Passthrough Architecture**: Delegate all operations to agent backends
3. **Thread Safety**: Concurrent request handling with safe handler updates
4. **Authentication**: Integrated auth middleware for all requests
5. **Protocol Compliance**: Full A2A protocol support
6. **Streaming Support**: Real-time message streaming
7. **Task Management**: Query, cancel, resubscribe operations
8. **Automatic Defaults**: Message ID and kind generation

## Design Patterns

### Multiplexer Pattern
The handlerMux acts as a dynamic HTTP multiplexer, routing requests to different handlers based on agent identity.

### Delegation Pattern
PassthroughManager delegates all operations to the underlying A2A client, providing a thin abstraction layer.

### Strategy Pattern
The A2A client strategy varies per agent (different backend URLs, configurations) while the interface remains consistent.

## Future Enhancements

- **Caching**: Cache agent handlers for improved routing performance
- **Metrics**: Track request counts, latencies per agent
- **Load Balancing**: Distribute requests across multiple agent instances
- **Circuit Breaker**: Prevent cascading failures from unhealthy agents
- **Request Logging**: Detailed logging of A2A protocol interactions
- **WebSocket Support**: Alternative transport for streaming
- **Protocol Versioning**: Support multiple A2A protocol versions
