# MCP Toolset Cleanup Solution

## Problem Summary

When making requests to an existing chat session, the code replaces MCP toolsets on every request. The old toolsets have active HTTP connections (SSE streams) that need to be closed. When Python's garbage collector cleans up these old toolsets in a background task, it tries to close async connections, causing:

```
RuntimeError: Attempted to exit cancel scope in a different task than it was entered in
```

This happens because:
- MCP connections use `anyio.create_task_group()` which creates task-bound cancel scopes
- Connection opened in: Request Task A
- Cleanup attempted in: GC Task B (background)
- `anyio` requires cancel scopes to be exited in the same task they were entered

## Solution

**Yes, we can close connections before starting new ones!**

We've modified `create_runner()` to:

1. **Make it async** - Allows us to await cleanup operations
2. **Explicitly close old toolsets** - Before creating new ones, close old MCP toolset connections
3. **Cleanup in same async context** - Ensures cleanup happens in the same task as the request

## Implementation

### Changes Made

**File:** `python/packages/kagent-adk/src/kagent/adk/_a2a.py`

**Before:**
```python
def create_runner() -> Runner:
    root_agent_instance = self.root_agent()
    if isinstance(root_agent_instance, LlmAgent) and root_agent_instance.tools:
        # Create new toolsets, old ones orphaned
        tools = []
        for tool in root_agent_instance.tools:
            # ... replacement logic ...
```

**After:**
```python
async def create_runner() -> Runner:
    root_agent_instance = self.root_agent()
    if isinstance(root_agent_instance, LlmAgent) and root_agent_instance.tools:
        # First, explicitly close old MCP toolsets before creating new ones
        old_toolsets = []
        for tool in root_agent_instance.tools:
            if isinstance(tool, McpToolset):
                old_toolsets.append(tool)
        
        # Close old toolsets in the current async context
        for old_tool in old_toolsets:
            try:
                if hasattr(old_tool, '_mcp_session_manager'):
                    session_manager = old_tool._mcp_session_manager
                    if hasattr(session_manager, 'close'):
                        await session_manager.close()
            except Exception as e:
                logger.warning(f"Failed to close old MCP toolset: {e}")
        
        # Now create new toolsets (existing logic)
        # ...
```

### How It Works

1. **Request arrives** → `A2aAgentExecutor._resolve_runner()` is called
2. **`_resolve_runner()` detects async callable** → Calls `create_runner()` and awaits it
3. **`create_runner()` closes old toolsets** → In the same async task as the request
4. **New toolsets created** → Old toolsets are properly closed, no GC race condition
5. **Runner returned** → Request proceeds normally

### Key Benefits

✅ **No async context violations** - Cleanup happens in the same task as the request  
✅ **Proper resource management** - Connections are explicitly closed  
✅ **No error noise** - Eliminates the cancel scope errors from logs  
✅ **Backward compatible** - `_resolve_runner()` already handles async callables  

### Error Handling

The cleanup is wrapped in try/except to be resilient:
- If cleanup fails, we log a warning but continue
- This prevents a single failed cleanup from breaking the request
- Old toolsets will still be garbage collected if cleanup fails (but without the error)

## Testing

To verify the fix works:

1. **Monitor logs** - Should no longer see cancel scope errors
2. **Check connection cleanup** - Should see debug logs: "Closed MCP toolset connection"
3. **Verify requests complete** - Requests should work normally
4. **Check resource usage** - Connections should be properly closed

## Expected Log Output

**Before fix:**
```
ERROR - RuntimeError: Attempted to exit cancel scope in a different task
WARNING - Session termination failed: 202
```

**After fix:**
```
DEBUG - Closed MCP toolset connection: 0xffff9844ab10
INFO - Replaced 3 MCP toolsets with new instances
```

## Notes

- The `_resolve_runner()` method in `_agent_executor.py` already handles async callables correctly
- No changes needed to the executor - it automatically awaits async `create_runner()`
- The cleanup is best-effort - if it fails, we log but don't break the request
- This solution ensures cleanup happens in the correct async context, preventing the task isolation violation

