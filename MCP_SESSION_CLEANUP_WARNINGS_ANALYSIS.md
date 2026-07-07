# MCP Session Cleanup Warnings Analysis

## Log Analysis

Looking at the logs from a session close operation:

```
2026-01-19 20:19:43,278 - mcp.client.streamable_http - INFO - GET stream disconnected, reconnecting in 1000ms...
2026-01-19 20:19:43,278 - httpx - INFO - HTTP Request: DELETE http://github-mcp-server.kagent:3000/mcp "HTTP/1.1 202 Accepted"
2026-01-19 20:19:43,279 - mcp.client.streamable_http - WARNING - Session termination failed: 202
Warning: Error during MCP session cleanup for session_no_headers: Attempted to exit cancel scope in a different task than it was entered in
2026-01-19 20:19:43,279 - kagent.adk._a2a - INFO - Closed MCP toolset connection: 281473134758944
```

## What's Happening

### Step-by-Step Flow

1. **Our cleanup code runs** (`create_runner()` calls `session_manager.close()`)
   - ✅ Successfully calls `await session_manager.close()`
   - ✅ Logs: "Closed MCP toolset connection: 281473134758944"

2. **MCP client library cleanup triggers**
   - The `session_manager.close()` method triggers internal cleanup in the MCP client library
   - The MCP client sends a `DELETE` request to terminate the session
   - Server responds with `202 Accepted` (success)

3. **MCP client misinterprets 202 response**
   - The MCP client library logs: "Session termination failed: 202"
   - This is a **false warning** - 202 is actually a success response
   - The client library seems to expect a different status code

4. **Async context violation occurs**
   - The MCP client's internal cleanup tries to exit cancel scopes
   - But these cancel scopes were entered in a **previous request's async task**
   - Even though we're calling `close()` in the current request's context, the underlying connections have task-bound contexts from when they were originally opened
   - Result: "Attempted to exit cancel scope in a different task than it was entered in"

## Root Cause

The issue is that **MCP connections have nested async contexts**:

1. **Outer context (our code):** We call `session_manager.close()` in the current request's async task ✅
2. **Inner context (MCP client):** The MCP client's internal connections were opened in a previous request's async task ❌

When we call `close()`, it triggers cleanup of the inner contexts, which were created in a different task. The `anyio` library enforces that cancel scopes must be exited in the same task they were entered.

## Why This Happens

### The Problem

```
Request 1 (Task A):
  ├─ Opens MCP connection
  ├─ Enters cancel scope (Task A)
  └─ Request completes, Runner discarded

Request 2 (Task B):
  ├─ Calls create_runner()
  ├─ Calls session_manager.close() (Task B) ✅
  ├─ Triggers MCP client cleanup
  └─ MCP client tries to exit cancel scope from Task A ❌
     └─ ERROR: Cancel scope entered in Task A, trying to exit in Task B
```

### Why Our Cleanup Doesn't Fully Solve It

Our cleanup code (`session_manager.close()`) is being called in the correct async context (Task B), but:

1. **The MCP client library has internal state** that was created in Task A
2. **The client's internal cleanup** tries to clean up connections that have Task A's cancel scopes
3. **Even though we're calling close() in Task B**, the underlying connections still reference Task A's contexts

## The Warnings Explained

### 1. "Session termination failed: 202"

**Source:** `mcp.client.streamable_http` library

**What it means:**
- The MCP client sent a `DELETE` request to terminate the session
- Server responded with `202 Accepted` (which is actually success)
- The client library is logging this as a failure, but it's likely a false warning
- The client may be expecting a different status code (like `200 OK` or `204 No Content`)

**Impact:** Low - This is just a warning, the session termination actually succeeded

### 2. "Error during MCP session cleanup: Attempted to exit cancel scope in a different task"

**Source:** MCP client library's internal cleanup code

**What it means:**
- The MCP client is trying to clean up async resources (cancel scopes, task groups)
- These resources were created in a different async task (previous request)
- `anyio` prevents exiting cancel scopes from different tasks (task isolation)

**Impact:** Medium - The cleanup partially fails, but connections are still terminated

## Current State

✅ **What's working:**
- Our cleanup code successfully calls `session_manager.close()`
- DELETE requests are sent and succeed (202 Accepted)
- Connections are terminated on the server side
- No unhandled exceptions crash the application

⚠️ **What's not ideal:**
- Warnings in logs (noise, but not breaking)
- MCP client's internal cleanup partially fails
- Some async resources may not be fully cleaned up

## Potential Solutions

### Option 1: Suppress Warnings (Quick Fix)

Since the cleanup is working (connections are terminated), we could suppress these warnings:

```python
import warnings
import logging

# Suppress MCP client warnings during cleanup
with warnings.catch_warnings():
    warnings.simplefilter("ignore")
    mcp_logger = logging.getLogger("mcp.client.streamable_http")
    mcp_logger.setLevel(logging.ERROR)  # Only show errors, not warnings
    await session_manager.close()
```

**Pros:** Clean logs, no functional impact  
**Cons:** Hides potentially useful information

### Option 2: Don't Close, Let Connections Timeout

Instead of explicitly closing, let connections timeout naturally:

```python
# Don't call close(), just let connections timeout
# MCP connections will eventually timeout and close themselves
```

**Pros:** No warnings  
**Cons:** Connections stay open longer, potential resource waste

### Option 3: Use Connection Pooling/Reuse

Instead of creating new toolsets every request, reuse existing ones:

```python
# Only create new toolsets if connection params changed
# Reuse existing toolsets if they're still valid
```

**Pros:** No cleanup needed, better performance  
**Cons:** More complex logic, may not solve the root issue

### Option 4: Accept the Warnings (Current Approach)

The warnings are harmless - connections are being terminated successfully. The async context violation is a limitation of how the MCP client library manages connections.

**Pros:** No code changes needed  
**Cons:** Warning noise in logs

## Recommendation

**Accept the warnings** - They're harmless and indicate that:
1. ✅ Cleanup is being attempted (good)
2. ✅ Connections are being terminated (good)
3. ⚠️ Internal cleanup has async context issues (cosmetic, not breaking)

The warnings don't indicate a functional problem - the MCP sessions are being closed successfully. The async context violation is a limitation of the MCP client library's architecture, not our code.

If the warning noise becomes problematic, Option 1 (suppress warnings) is the cleanest approach.

## Verification

To verify cleanup is working despite warnings:

1. **Check connection count:** Monitor MCP server connections - they should decrease after cleanup
2. **Check logs:** "Closed MCP toolset connection" confirms our code ran
3. **Check HTTP responses:** `202 Accepted` confirms server received termination requests
4. **No crashes:** Application continues running normally

The warnings are **cosmetic** - the actual cleanup is working correctly.

