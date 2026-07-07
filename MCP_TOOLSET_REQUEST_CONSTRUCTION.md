# MCP Toolset HTTP Request Construction - Complete Trace

This document provides a detailed trace of how `McpToolset` constructs HTTP POST requests to MCP servers, based on the actual source code from the Google ADK library and MCP Python SDK.

## Quick Reference: First Request Example

For the URL `http://kagent-grafana-mcp.kagent:8000/mcp` with tools `["create_alert_rule", "create_incident", "create_folder"]`, the **first HTTP request** that httpx logs is:

```http
POST http://kagent-grafana-mcp.kagent:8000/mcp HTTP/1.1
Host: kagent-grafana-mcp.kagent:8000
Accept: application/json, text/event-stream
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {
      "name": "google-adk",
      "version": "1.21.0"
    }
  }
}
```

This is logged by httpx as: `httpx - INFO - HTTP Request: POST http://kagent-grafana-mcp.kagent:8000/mcp`

**Important:** Tool names (`create_alert_rule`, `create_incident`, `create_folder`) do NOT affect the HTTP requests themselves. They are used as a filter AFTER getting all tools from the server. The `tools/list` request still retrieves all available tools, but only the specified tools are exposed to the agent.

## Overview

When an agent uses an MCP tool, the following flow occurs:

1. **McpToolset** → Creates **MCPSessionManager** → Creates **MCP ClientSession** → Uses **StreamableHTTPTransport** → Makes **httpx POST request**

The first request is always `initialize`, followed by `tools/list`, then `tools/call` when tools are actually used.

## Step-by-Step Request Construction

### Step 1: McpToolset Initialization

**File:** `google/adk/tools/mcp_tool/mcp_toolset.py`

```python
# Line 120-123 in kagent/adk/types.py
McpToolset(
    connection_params=http_tool.params,  # StreamableHTTPConnectionParams
    tool_filter=http_tool.tools,
    header_provider=header_provider
)
```

**Key Code:** `mcp_toolset.py:137-140`
```python
self._mcp_session_manager = MCPSessionManager(
    connection_params=self._connection_params,  # StreamableHTTPConnectionParams
    errlog=self._errlog,
)
```

**Connection Params Structure:**
```python
StreamableHTTPConnectionParams(
    url="http://kagent-grafana-mcp.kagent:8000/mcp",
    headers={},  # or {"x-kagent-host": "..."} if proxy
    timeout=5.0,
    sse_read_timeout=300.0,
    terminate_on_close=True
)
```

**Tool Filter:** The tool names are passed as `tool_filter`:
```python
McpToolset(
    connection_params=StreamableHTTPConnectionParams(...),
    tool_filter=["create_alert_rule", "create_incident", "create_folder"],  # Filter applied client-side
    header_provider=header_provider
)
```

### Step 2: MCPSessionManager Creates Client

**File:** `google/adk/tools/mcp_tool/mcp_session_manager.py`

**Key Code:** `mcp_session_manager.py:279-288`
```python
elif isinstance(self._connection_params, StreamableHTTPConnectionParams):
    client = streamablehttp_client(
        url=self._connection_params.url,
        headers=merged_headers,  # Base headers + any additional headers
        timeout=timedelta(seconds=self._connection_params.timeout),
        sse_read_timeout=timedelta(seconds=self._connection_params.sse_read_timeout),
        terminate_on_close=self._connection_params.terminate_on_close,
    )
```

**Header Merging:** `mcp_session_manager.py:214-241`
```python
def _merge_headers(self, additional_headers: Optional[Dict[str, str]] = None):
    base_headers = {}
    if hasattr(self._connection_params, 'headers') and self._connection_params.headers:
        base_headers = self._connection_params.headers.copy()
    
    if additional_headers:
        base_headers.update(additional_headers)
    
    return base_headers
```

### Step 3: StreamableHTTPTransport Initialization

**File:** `mcp/client/streamable_http.py`

**Key Code:** `streamable_http.py:77-107`
```python
class StreamableHTTPTransport:
    def __init__(
        self,
        url: str,
        headers: dict[str, str] | None = None,
        timeout: float | timedelta = 30,
        sse_read_timeout: float | timedelta = 60 * 5,
        auth: httpx.Auth | None = None,
    ):
        self.url = url  # "http://kagent-grafana-mcp.kagent:8000/mcp"
        self.headers = headers or {}  # Custom headers from connection params
        self.timeout = timeout.total_seconds() if isinstance(timeout, timedelta) else timeout
        self.sse_read_timeout = sse_read_timeout.total_seconds() if isinstance(sse_read_timeout, timedelta) else sse_read_timeout
        
        # Request headers are prepared here
        self.request_headers = {
            ACCEPT: f"{JSON}, {SSE}",  # "application/json, text/event-stream"
            CONTENT_TYPE: JSON,  # "application/json"
            **self.headers,  # Custom headers merged in
        }
```

**Constants:** `streamable_http.py:49-50`
```python
JSON = "application/json"
SSE = "text/event-stream"
```

### Step 4: HTTP Client Creation

**Key Code:** `streamable_http.py:480-484`
```python
async with httpx_client_factory(
    headers=transport.request_headers,
    timeout=httpx.Timeout(transport.timeout, read=transport.sse_read_timeout),
    auth=transport.auth,
) as client:
```

**Default Factory:** Uses `create_mcp_http_client` from `mcp.shared._httpx_utils`, which creates:
```python
httpx.AsyncClient(
    headers=request_headers,  # Includes Accept, Content-Type, custom headers
    timeout=httpx.Timeout(timeout, read=sse_read_timeout),
    auth=auth,
)
```

### Step 5: POST Request Construction

**Key Code:** `streamable_http.py:254-265` - `_handle_post_request()`

```python
async def _handle_post_request(self, ctx: RequestContext) -> None:
    """Handle a POST request with response processing."""
    headers = self._prepare_request_headers(ctx.headers)
    message = ctx.session_message.message
    is_initialization = self._is_initialization_request(message)

    async with ctx.client.stream(
        "POST",
        self.url,  # "http://kagent-grafana-mcp.kagent:8000/mcp"
        json=message.model_dump(by_alias=True, mode="json", exclude_none=True),
        headers=headers,
    ) as response:
        # ... handle response
```

**Header Preparation:** `streamable_http.py:109-116`
```python
def _prepare_request_headers(self, base_headers: dict[str, str]) -> dict[str, str]:
    """Update headers with session ID and protocol version if available."""
    headers = base_headers.copy()
    if self.session_id:
        headers[MCP_SESSION_ID] = self.session_id  # "mcp-session-id"
    if self.protocol_version:
        headers[MCP_PROTOCOL_VERSION] = self.protocol_version  # "mcp-protocol-version"
    return headers
```

### Step 6: Tool Filtering (Client-Side)

**Important:** Tool names (`["create_alert_rule", "create_incident", "create_folder"]`) do NOT affect HTTP requests. They are used for client-side filtering.

**Key Code:** `mcp_toolset.py:162-175`
```python
# Fetch available tools from the MCP server
# NOTE: This gets ALL tools from the server, regardless of tool_filter
tools_response: ListToolsResult = await session.list_tools()

# Apply filtering based on context and tool_filter
tools = []
for tool in tools_response.tools:
  mcp_tool = MCPTool(...)
  
  # Filter: Only include tools that match tool_filter
  # For tool_filter=["create_alert_rule", "create_incident", "create_folder"],
  # only tools with these names are included
  if self._is_tool_selected(mcp_tool, readonly_context):
    tools.append(mcp_tool)
```

**Filtering Logic:** `base_toolset.py:186-187`
```python
if isinstance(self.tool_filter, list):
    return tool.name in self.tool_filter  # Checks if tool.name is in the filter list
```

**Impact on HTTP Requests:**
- `tools/list` request: **Unchanged** - still retrieves all tools from server
- Tool filtering: **Client-side only** - happens after receiving `tools/list` response
- `tools/call` request: Uses the **specific tool name** when agent calls a tool

### Step 7: JSON-RPC Message Construction

**Message Format:** The `message` is a `JSONRPCMessage` containing a `JSONRPCRequest`:

```python
JSONRPCRequest(
    jsonrpc="2.0",
    id=1,  # Request ID
    method="tools/call",
    params={
        "name": "create_alert_rule",  # Specific tool name from filter
        "arguments": {
            "name": "High CPU Usage",
            "condition": "cpu_usage > 80",
            "folder": "production"
        }
    }
)
```

**Serialization:** `message.model_dump(by_alias=True, mode="json", exclude_none=True)` converts this to JSON.

## Complete HTTP Request Examples

### Example 1: First Request - Initialize (Session Setup)

For URL `http://kagent-grafana-mcp.kagent:8000/mcp`, the **first HTTP request** is the `initialize` request:

```http
POST http://kagent-grafana-mcp.kagent:8000/mcp HTTP/1.1
Host: kagent-grafana-mcp.kagent:8000
Accept: application/json, text/event-stream
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {
      "name": "google-adk",
      "version": "1.21.0"
    }
  }
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789
mcp-protocol-version: 2024-11-05

{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2024-11-05",
    "serverInfo": {
      "name": "grafana-mcp",
      "version": "1.0.0"
    },
    "capabilities": {
      "tools": {
        "listChanged": false
      }
    }
  }
}
```

**Note:** After this response, the client stores the `mcp-session-id` and `mcp-protocol-version` from headers for subsequent requests.

### Example 2: Second Request - List Tools

After initialization, the client requests available tools. **Note:** This request retrieves ALL tools from the server, regardless of the tool filter:

```http
POST http://kagent-grafana-mcp.kagent:8000/mcp HTTP/1.1
Host: kagent-grafana-mcp.kagent:8000
Accept: application/json, text/event-stream
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789
mcp-protocol-version: 2024-11-05

{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/list",
  "params": {}
}
```

**Response:** (Server returns all available tools)
```http
HTTP/1.1 200 OK
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789

{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "create_alert_rule",
        "description": "Create a new alert rule in Grafana",
        "inputSchema": {
          "type": "object",
          "properties": {
            "name": {"type": "string"},
            "condition": {"type": "string"},
            "folder": {"type": "string"}
          }
        }
      },
      {
        "name": "create_incident",
        "description": "Create a new incident",
        "inputSchema": {
          "type": "object",
          "properties": {
            "title": {"type": "string"},
            "description": {"type": "string"},
            "severity": {"type": "string"}
          }
        }
      },
      {
        "name": "create_folder",
        "description": "Create a new folder",
        "inputSchema": {
          "type": "object",
          "properties": {
            "name": {"type": "string"},
            "parent": {"type": "string"}
          }
        }
      },
      {
        "name": "delete_alert_rule",
        "description": "Delete an alert rule",
        "inputSchema": {
          "type": "object",
          "properties": {
            "id": {"type": "string"}
          }
        }
      }
    ]
  }
}
```

**Filtering:** After receiving this response, `McpToolset` filters the tools using `tool_filter=["create_alert_rule", "create_incident", "create_folder"]`. Only these three tools are exposed to the agent, even though the server returned four tools. The filtering happens in `mcp_toolset.py:174` via `_is_tool_selected()` which checks `tool.name in self.tool_filter`.

### Example 3: Tool Call Requests

When the agent actually uses a tool, the request uses the specific tool name. Here are examples for each of the three filtered tools:

#### 3a: Calling `create_alert_rule`

```http
POST http://kagent-grafana-mcp.kagent:8000/mcp HTTP/1.1
Host: kagent-grafana-mcp.kagent:8000
Accept: application/json, text/event-stream
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789
mcp-protocol-version: 2024-11-05

{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "create_alert_rule",
    "arguments": {
      "name": "High CPU Usage",
      "condition": "cpu_usage > 80",
      "folder": "production"
    }
  }
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789

{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Alert rule 'High CPU Usage' created successfully with ID: alert-123"
      }
    ],
    "isError": false
  }
}
```

#### 3b: Calling `create_incident`

```http
POST http://kagent-grafana-mcp.kagent:8000/mcp HTTP/1.1
Host: kagent-grafana-mcp.kagent:8000
Accept: application/json, text/event-stream
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789
mcp-protocol-version: 2024-11-05

{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "create_incident",
    "arguments": {
      "title": "Service Outage",
      "description": "API service is down",
      "severity": "critical"
    }
  }
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789

{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Incident 'Service Outage' created with ID: INC-456"
      }
    ],
    "isError": false
  }
}
```

#### 3c: Calling `create_folder`

```http
POST http://kagent-grafana-mcp.kagent:8000/mcp HTTP/1.1
Host: kagent-grafana-mcp.kagent:8000
Accept: application/json, text/event-stream
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789
mcp-protocol-version: 2024-11-05

{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "create_folder",
    "arguments": {
      "name": "monitoring",
      "parent": "/"
    }
  }
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/json
mcp-session-id: abc123-session-id-xyz789

{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Folder 'monitoring' created successfully"
      }
    ],
    "isError": false
  }
}
```

**Note:** Only tools in the filter (`create_alert_rule`, `create_incident`, `create_folder`) can be called. If the agent tries to call `delete_alert_rule` (which was filtered out), it won't be available to the agent even though the server supports it.

### Example 4: With Proxy Configuration

If proxy is configured, the URL changes but headers include routing info:

```http
POST http://proxy.kagent.svc.cluster.local:8080/mcp HTTP/1.1
Host: proxy.kagent.svc.cluster.local:8080
Accept: application/json, text/event-stream
Content-Type: application/json
x-kagent-host: kagent-grafana-mcp.kagent

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {
      "name": "google-adk",
      "version": "1.21.0"
    }
  }
}
```

**Note:** The `x-kagent-host` header tells the proxy where to route the request.

## Key Headers Explained

1. **Accept: application/json, text/event-stream**
   - Set automatically by `StreamableHTTPTransport`
   - Indicates client can handle both JSON and SSE responses

2. **Content-Type: application/json**
   - Set automatically by `StreamableHTTPTransport`
   - Indicates request body is JSON

3. **mcp-session-id** (optional)
   - Added after initial session establishment
   - Used for session management

4. **mcp-protocol-version** (optional)
   - Added after initialization handshake
   - Indicates negotiated protocol version

5. **Custom Headers**
   - From `StreamableHTTPConnectionParams.headers`
   - Example: `x-kagent-host: kagent-grafana-mcp.kagent` (for proxy routing)

## httpx Logging

When httpx logs:
```
httpx - INFO - HTTP Request: POST http://kagent-grafana-mcp.kagent:8000/mcp
```

This happens in httpx's internal logging when `ctx.client.stream("POST", ...)` is called at `streamable_http.py:260`.

## Code Flow Summary

```
AgentConfig.to_agent()
  └─> Creates McpToolset(connection_params=StreamableHTTPConnectionParams(...))
      └─> Creates MCPSessionManager(connection_params)
          └─> Calls streamablehttp_client(url, headers, timeout, ...)
              └─> Creates StreamableHTTPTransport(url, headers, ...)
                  └─> Creates httpx.AsyncClient(headers=request_headers, ...)
                      └─> When tool is called:
                          └─> Creates JSONRPCRequest(method="tools/call", ...)
                              └─> Calls client.stream("POST", url, json=message, headers=headers)
                                  └─> httpx logs: "HTTP Request: POST http://..."
```

## Important Files

1. **Google ADK:**
   - `/Library/Frameworks/Python.framework/Versions/3.11/lib/python3.11/site-packages/google/adk/tools/mcp_tool/mcp_toolset.py`
   - `/Library/Frameworks/Python.framework/Versions/3.11/lib/python3.11/site-packages/google/adk/tools/mcp_tool/mcp_session_manager.py`

2. **MCP Python SDK:**
   - `/Library/Frameworks/Python.framework/Versions/3.11/lib/python3.11/site-packages/mcp/client/streamable_http.py`

3. **KAgent Code:**
   - `python/packages/kagent-adk/src/kagent/adk/types.py:120` - Creates McpToolset
   - `go/internal/controller/translator/agent/adk_api_translator.go:939` - Creates StreamableHTTPConnectionParams

## Session Management

The `MCPSessionManager` implements session pooling:
- Sessions are keyed by connection params + headers hash
- Reuses existing sessions if still connected
- Creates new session if disconnected
- Handles session initialization automatically

## Error Handling

- **404 Response:** Sends session terminated error
- **202 Accepted:** Returns immediately (async processing)
- **SSE Response:** Handles streaming responses via Server-Sent Events
- **JSON Response:** Parses and forwards JSON-RPC response

## Proxy Configuration

When proxy is configured (via `applyProxyURL` in Go translator):
- URL is rewritten to proxy URL: `http://proxy.kagent.svc.cluster.local:8080/mcp`
- Original hostname added as header: `x-kagent-host: kagent-grafana-mcp.kagent`
- Headers merged into `StreamableHTTPConnectionParams.headers`
- All subsequent requests use proxy URL with routing header

