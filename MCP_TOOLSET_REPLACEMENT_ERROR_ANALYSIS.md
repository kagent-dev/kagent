# MCP Toolset Replacement Error Analysis

## Log Analysis: Step-by-Step Request Flow

### Step 1: New Request Arrives - Runner Creation (22:14:52,431)

```
2026-01-17 22:14:52,431 - kagent.adk._a2a - INFO - Creating new Runner instance - calling root_agent factory
2026-01-17 22:14:52,431 - kagent.adk._a2a - INFO - Root agent factory returned agent: LlmAgent with ID: 0xffff99365d60
```

**What's happening:**
- A new request arrives for an existing chat session (`ctx-58caa17a-ee3e-4ece-8f6f-2a931b5a6120`)
- `A2aAgentExecutor._resolve_runner()` calls `create_runner()` function
- `create_runner()` calls `self.root_agent()` factory to get a fresh agent instance
- The agent factory returns a new `LlmAgent` instance (ID: `0xffff99365d60`)

**Code location:** `_a2a.py:83-84`

### Step 2: MCP Toolset Replacement (22:14:52,431)

```
2026-01-17 22:14:52,431 - kagent.adk._a2a - INFO - Replaced MCP toolset - Old ID: 0xffff9844ab10, New ID: 0xffff98437df0, URL: http://github-mcp-server.kagent:3000/mcp
2026-01-17 22:14:52,431 - kagent.adk._a2a - INFO - Replaced MCP toolset - Old ID: 0xffff9844bbb0, New ID: 0xffff98310050, URL: http://github-mcp-server.kagent:3000/mcp
2026-01-17 22:14:52,431 - kagent.adk._a2a - INFO - Replaced MCP toolset - Old ID: 0xffff984a1c70, New ID: 0xffff984b4150, URL: http://github-mcp-server.kagent:3000/mcp
2026-01-17 22:14:52,431 - kagent.adk._a2a - INFO - Replaced 3 MCP toolsets with new instances
```

**What's happening:**
- The agent from the factory has 3 existing `McpToolset` instances (from previous requests)
- The replacement logic (`_a2a.py:85-108`) extracts connection params from each old toolset
- Creates 3 **new** `McpToolset` instances with the same connection params
- Replaces the old toolsets with new ones on the agent
- **Critical:** The old toolset instances are now orphaned and will be garbage collected

**Code location:** `_a2a.py:88-108`

**The Problem:**
- Old MCP toolsets have active HTTP connections (SSE streams) to the MCP server
- These connections are managed by async context managers (anyio task groups)
- When Python garbage collects the old toolsets, their cleanup code runs
- But cleanup happens in a **different async task** than where the connection was opened

### Step 3: Task and Session Lookup (22:14:52,445-452)

```
2026-01-17 22:14:52,445 - httpx - INFO - HTTP Request: GET http://kagent-controller.kagent:8083/api/tasks/bfba1f51-a1a9-49f5-9e46-161419889b38 "HTTP/1.1 404 Not Found"
2026-01-17 22:14:52,446 - httpx - INFO - HTTP Request: GET http://kagent-controller.kagent:8083/api/sessions/ctx-58caa17a-ee3e-4ece-8f6f-2a931b5a6120?user_id=acf04f2b-f529-45ef-a2d8-5311fc3c8301&limit=-1 "HTTP/1.1 200 OK"
2026-01-17 22:14:52,446 - a2a.server.tasks.task_manager - INFO - Task not found or task_id not set. Creating new task for event (task_id: bfba1f51-a1a9-49f5-9e46-161419889b38, context_id: ctx-58caa17a-ee3e-4ece-8f6f-2a931b5a6120).
2026-01-17 22:14:52,452 - httpx - INFO - HTTP Request: POST http://kagent-controller.kagent:8083/api/tasks "HTTP/1.1 201 Created"
```

**What's happening:**
- `DefaultRequestHandler` looks up the task (not found - new task)
- Looks up the existing session (found - continuing conversation)
- Creates a new task record for this request

**Code location:** `DefaultRequestHandler` → `KAgentTaskStore`

### Step 4: Session Event Appending (22:14:52,456-474)

```
2026-01-17 22:14:52,456 - httpx - INFO - HTTP Request: POST http://kagent-controller.kagent:8083/api/sessions/ctx-58caa17a-ee3e-4ece-8f6f-2a931b5a6120/events?user_id=acf04f2b-f529-45ef-a2d8-5311fc3c8301 "HTTP/1.1 201 Created"
2026-01-17 22:14:52,461 - httpx - INFO - HTTP Request: POST http://kagent-controller.kagent:8083/api/tasks "HTTP/1.1 201 Created"
2026-01-17 22:14:52,465 - httpx - INFO - HTTP Request: GET http://kagent-controller.kagent:8083/api/sessions/ctx-58caa17a-ee3e-4ece-8f6f-2a931b5a6120?user_id=acf04f2b-f529-45ef-a2d8-5311fc3c8301&limit=-1 "HTTP/1.1 200 OK"
2026-01-17 22:14:52,473 - httpx - INFO - HTTP Request: POST http://kagent-controller.kagent:8083/api/sessions/ctx-58caa17a-ee3e-4ece-8f6f-2a931b5a6120/events?user_id=acf04f2b-f529-45ef-a2d8-5311fc3c8301 "HTTP/1.1 201 Created"
2026-01-17 22:14:52,474 - httpx - INFO - HTTP Request: POST http://kagent-controller.kagent:8083/api/tasks "HTTP/1.1 201 Created"
```

**What's happening:**
- `A2aAgentExecutor._handle_request()` prepares the session
- Appends system events (header updates) to the session
- Publishes task status events (submitted, working)

**Code location:** `_agent_executor.py:196-237`

### Step 5: Unknown Agent Warning (22:14:52,474)

```
2026-01-17 22:14:52,474 - google_adk.google.adk.runners - WARNING - Event from an unknown agent: system, event id: 3ad740a8-2212-43e1-9ee6-348ec9636339
2026-01-17 22:14:52,474 - google_adk.google.adk.runners - WARNING - Event from an unknown agent: system, event id: 3ad740a8-2212-43e1-9ee6-348ec9636339
```

**What's happening:**
- The Runner is processing system events
- These warnings are benign - the Runner doesn't recognize the "system" agent
- This is expected behavior when appending header update events

### Step 6: Old MCP Toolset Cleanup Begins (22:14:52,508-512)

```
2026-01-17 22:14:52,508 - httpx - INFO - HTTP Request: DELETE http://github-mcp-server.kagent:3000/mcp "HTTP/1.1 202 Accepted"
2026-01-17 22:14:52,509 - httpx - INFO - HTTP Request: DELETE http://github-mcp-server.kagent:3000/mcp "HTTP/1.1 202 Accepted"
2026-01-17 22:14:52,510 - mcp.client.streamable_http - WARNING - Session termination failed: 202
2026-01-17 22:14:52,511 - mcp.client.streamable_http - INFO - GET stream disconnected, reconnecting in 1000ms...
2026-01-17 22:14:52,512 - mcp.client.streamable_http - WARNING - Session termination failed: 202
2026-01-17 22:14:52,512 - mcp.client.streamable_http - INFO - GET stream disconnected, reconnecting in 1000ms...
```

**What's happening:**
- Python's garbage collector is cleaning up the **old** MCP toolset instances
- Each old `McpToolset` has an active MCP connection (managed by `MCPSessionManager`)
- When the toolset is garbage collected, its `__del__` or cleanup method is called
- This triggers `DELETE` requests to terminate the MCP sessions
- The MCP client receives 202 (Accepted) responses
- The client tries to reconnect (because it doesn't understand the termination)

**The Root Cause:**
- Old MCP toolsets are being garbage collected **asynchronously**
- The cleanup happens in a different async task context than where the connection was opened
- MCP connections use `anyio.create_task_group()` which requires cleanup in the same task

### Step 7: Cancel Scope Error (22:14:52,515-520)

```
2026-01-17 22:14:52,515 - asyncio - ERROR - Task exception was never retrieved
future: <Task finished name='Task-100' coro=<<async_generator_athrow without __name__>()> exception=RuntimeError('Attempted to exit cancel scope in a different task than it was entered in')>
```

**Error Details:**
```
RuntimeError: Attempted to exit cancel scope in a different task than it was entered in
```

**What's happening:**
- The old MCP toolset cleanup is trying to exit an `anyio` cancel scope
- But the cancel scope was entered in a different async task (the original request)
- `anyio` enforces that cancel scopes must be exited in the same task they were entered
- This is a **task isolation violation** in async context management

**Technical Explanation:**
1. **Original Request Task:** Opens MCP connection → enters cancel scope (Task A)
2. **New Request:** Creates new Runner → replaces toolsets → old toolsets orphaned
3. **Garbage Collection:** Python GC runs in background → calls cleanup (Task B)
4. **Cleanup Fails:** Tries to exit cancel scope in Task B, but scope was entered in Task A
5. **Error:** `RuntimeError: Attempted to exit cancel scope in a different task than it was entered in`

### Step 8: More Cleanup Attempts (22:14:52,536-537)

```
2026-01-17 22:14:52,536 - httpx - INFO - HTTP Request: DELETE http://github-mcp-server.kagent:3000/mcp "HTTP/1.1 202 Accepted"
2026-01-17 22:14:52,536 - mcp.client.streamable_http - WARNING - Session termination failed: 202
2026-01-17 22:14:52,537 - mcp.client.streamable_http - INFO - GET stream disconnected, reconnecting in 1000ms...
```

**What's happening:**
- More cleanup attempts as Python GC continues
- Same pattern: DELETE request → 202 response → reconnection attempt

## The Core Problem

### Why This Happens

1. **Per-Request Toolset Replacement:**
   - Every request creates a new Runner with new MCP toolset instances
   - Old toolsets are immediately orphaned (no references)
   - Python garbage collector cleans them up asynchronously

2. **Async Context Management:**
   - MCP connections use `anyio.create_task_group()` for connection management
   - Task groups create cancel scopes that are **task-bound**
   - Cleanup must happen in the **same async task** where the connection was opened

3. **Task Isolation Violation:**
   - Connection opened in: Request Task A (original request)
   - Cleanup attempted in: GC Task B (background garbage collection)
   - Result: Cancel scope error

### Why This Is Problematic

1. **Resource Leaks:** Old connections aren't properly closed
2. **Error Noise:** Exceptions logged but not handled
3. **Connection Waste:** MCP server receives unnecessary termination requests
4. **Potential Instability:** Unhandled exceptions in background tasks

## Solution Approaches

### Option 1: Explicit Cleanup Before Replacement (Recommended)

**Modify `create_runner()` to explicitly close old toolsets before creating new ones:**

```python
def create_runner() -> Runner:
    root_agent_instance = self.root_agent()
    
    if isinstance(root_agent_instance, LlmAgent) and root_agent_instance.tools:
        # Explicitly close old MCP toolsets before replacement
        for tool in root_agent_instance.tools:
            if isinstance(tool, McpToolset):
                # Close the connection in the current async context
                if hasattr(tool, '_mcp_session_manager'):
                    await tool._mcp_session_manager.close()
        
        # Then create new toolsets (existing code)
        tools = []
        for tool in root_agent_instance.tools:
            # ... replacement logic ...
```

**Problem:** `create_runner()` is not async, but cleanup needs to be async.

### Option 2: Track and Cleanup in Executor

**Modify `A2aAgentExecutor` to track and cleanup old Runners:**

```python
async def execute(self, context: RequestContext, event_queue: EventQueue):
    # ... existing code ...
    
    runner = await self._resolve_runner()
    try:
        await self._handle_request(context, event_queue, runner, run_args)
    finally:
        # Explicitly cleanup runner and its toolsets
        await self._cleanup_runner(runner)

async def _cleanup_runner(self, runner: Runner):
    """Cleanup runner resources including MCP toolsets."""
    if hasattr(runner, 'app') and hasattr(runner.app, 'root_agent'):
        agent = runner.app.root_agent
        if isinstance(agent, LlmAgent) and agent.tools:
            for tool in agent.tools:
                if isinstance(tool, McpToolset):
                    if hasattr(tool, '_mcp_session_manager'):
                        await tool._mcp_session_manager.close()
```

**Problem:** Need to ensure cleanup happens in the same async context.

### Option 3: Don't Replace Toolsets (Simplest)

**Only create new toolsets if they don't already exist:**

```python
def create_runner() -> Runner:
    root_agent_instance = self.root_agent()
    
    if isinstance(root_agent_instance, LlmAgent) and root_agent_instance.tools:
        # Check if toolsets need replacement (e.g., connection params changed)
        # Only replace if necessary, otherwise reuse existing toolsets
        tools = []
        for tool in root_agent_instance.tools:
            if isinstance(tool, McpToolset):
                # Check if we need to replace (e.g., connection params changed)
                if self._needs_replacement(tool):
                    # Create new toolset
                    # ... existing replacement logic ...
                else:
                    # Reuse existing toolset
                    tools.append(tool)
```

**Problem:** May not solve the original problem that necessitated replacement.

### Option 4: Use Weak References and Background Cleanup

**Track toolsets with weak references and cleanup in a background task:**

```python
import weakref
from asyncio import create_task

class KAgentApp:
    def __init__(self, ...):
        # ... existing code ...
        self._pending_cleanups = weakref.WeakSet()
    
    def build(self, local=False) -> FastAPI:
        # ... existing code ...
        
        def create_runner() -> Runner:
            root_agent_instance = self.root_agent()
            
            if isinstance(root_agent_instance, LlmAgent) and root_agent_instance.tools:
                # Track old toolsets for cleanup
                old_toolsets = [
                    tool for tool in root_agent_instance.tools 
                    if isinstance(tool, McpToolset)
                ]
                
                # Create new toolsets (existing code)
                # ... replacement logic ...
                
                # Schedule cleanup of old toolsets in background
                if old_toolsets:
                    create_task(self._cleanup_toolsets(old_toolsets))
```

**Problem:** Still has task isolation issues if not done carefully.

## Recommended Solution

**Hybrid Approach:** Make `create_runner()` async and cleanup old toolsets explicitly:

1. Make `create_runner()` async
2. Before creating new toolsets, explicitly close old ones in the current async context
3. Ensure cleanup happens in the same task as the request

This ensures:
- Old connections are properly closed
- Cleanup happens in the correct async context
- No garbage collection race conditions
- No cancel scope violations

## Impact Assessment

### Current Impact
- **Functional:** Requests still complete successfully
- **Operational:** Error noise in logs, potential resource leaks
- **Performance:** Unnecessary connection churn

### With Fix
- **Functional:** Same behavior, cleaner execution
- **Operational:** No error noise, proper resource management
- **Performance:** Reduced connection overhead

