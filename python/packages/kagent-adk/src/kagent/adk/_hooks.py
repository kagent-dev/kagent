"""
Hooks runtime for the kagent hooks system.

Implements the "claude-command" protocol used by kagent hooks:
  - The hook process reads a single JSON object from stdin.
  - The hook process writes a single JSON object to stdout, OR writes a message
    to stderr and exits with code 2 (signals a non-blocking error).

Supported events:
  - PreToolUse:   fires before a tool is invoked; can approve or block.
  - PostToolUse:  fires after a tool completes; informational only.
  - SessionStart: fires when a new agent session is created.
  - SessionEnd:   fires when an agent session completes.

See also:
  - HookSpec in go/api/v1alpha2/agent_types.go  (CRD definition)
  - HookConfig in go/api/adk/types.go            (Go serialization)
  - HookConfig in types.py                       (Python deserialization)
"""

from __future__ import annotations

import asyncio
import functools
import json
import logging
import os
import re
import subprocess
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from google.adk.tools.base_tool import BaseTool
    from google.adk.tools.tool_context import ToolContext

    from kagent.adk.types import HookConfig

logger = logging.getLogger(__name__)

# Maximum seconds to wait for a hook subprocess before treating it as an error.
_HOOK_TIMEOUT_SECONDS = 30


def _resolve_argv(hook: HookConfig) -> list[str]:
    """Build the argv list for the hook command.

    If the first token of command is not an absolute path, prepend hook.dir so
    the script can be found without the caller knowing the mount location.
    """
    parts = hook.command.split()
    if parts and not os.path.isabs(parts[0]):
        parts[0] = os.path.join(hook.dir, parts[0])
    return parts


def execute_hook_subprocess(hook: HookConfig, input_data: dict[str, Any]) -> dict[str, Any] | None:
    """Run a single hook command synchronously via subprocess.

    Protocol (claude-command):
      - stdin:  JSON-encoded input_data
      - stdout: JSON-encoded output dict (may be empty ``{}``)
      - exit 0: success; parse stdout as JSON
      - exit 2: hook-signalled error; log stderr as warning, return None
      - other:  unexpected error; log as error, return None

    Returns the parsed JSON output dict, or None when the hook cannot be run
    or explicitly signals an error (exit code 2). A None return is always
    non-blocking — the agent continues normally.
    """
    argv = _resolve_argv(hook)
    input_json = json.dumps(input_data)

    try:
        result = subprocess.run(
            argv,
            input=input_json,
            capture_output=True,
            text=True,
            timeout=_HOOK_TIMEOUT_SECONDS,
        )
    except FileNotFoundError:
        logger.error("Hook command not found: %r (dir=%s)", hook.command, hook.dir)
        return None
    except subprocess.TimeoutExpired:
        logger.error(
            "Hook command timed out after %ds: %r",
            _HOOK_TIMEOUT_SECONDS,
            hook.command,
        )
        return None
    except Exception as exc:
        logger.error("Unexpected error running hook %r: %s", hook.command, exc)
        return None

    if result.returncode == 2:
        stderr_msg = result.stderr.strip()
        logger.warning("Hook %r exited with code 2: %s", hook.command, stderr_msg)
        return None

    if result.returncode != 0:
        logger.error(
            "Hook %r exited with unexpected code %d: %s",
            hook.command,
            result.returncode,
            result.stderr.strip(),
        )
        return None

    if result.stderr:
        logger.debug("Hook %r stderr: %s", hook.command, result.stderr.strip())

    stdout = result.stdout.strip()
    if not stdout:
        return {}

    try:
        return json.loads(stdout)
    except json.JSONDecodeError as exc:
        logger.error("Hook %r produced invalid JSON: %s", hook.command, exc)
        return None


def _matches_tool(hook: HookConfig, tool_name: str) -> bool:
    """Return True if the hook's matcher regex matches tool_name, or no matcher is set."""
    if not hook.matcher:
        return True
    return bool(re.search(hook.matcher, tool_name))


def make_pre_tool_hook_callback(hooks: list[HookConfig]):
    """Return an ADK before_tool_callback that runs all PreToolUse hooks.

    The callback is synchronous (ADK requirement). Hooks run in declaration
    order. If any hook returns ``{"decision": "block"}`` the tool call is
    rejected with the supplied reason. A hook error (None return) is
    non-blocking by default.

    Returns None when there are no PreToolUse hooks (avoids wrapping overhead).
    """
    pre_hooks = [h for h in hooks if h.event == "PreToolUse"]
    if not pre_hooks:
        return None

    def before_tool(
        tool: BaseTool,
        args: dict[str, Any],
        tool_context: ToolContext,
    ) -> str | dict | None:
        tool_name = tool.name
        input_data: dict[str, Any] = {
            "hook_event_name": "PreToolUse",
            "tool_name": tool_name,
            "tool_input": args,
        }

        for hook in pre_hooks:
            if not _matches_tool(hook, tool_name):
                continue
            output = execute_hook_subprocess(hook, input_data)
            if output is None:
                continue  # hook error — non-blocking
            decision = output.get("decision", "approve")
            if decision == "block":
                reason = output.get("reason", "Tool execution blocked by hook.")
                logger.info("Hook %r blocked tool %r: %s", hook.command, tool_name, reason)
                return f"Tool execution blocked by hook: {reason}"

        return None  # all hooks approved

    return before_tool


def make_post_tool_hook_callback(hooks: list[HookConfig]):
    """Return an ADK after_tool_callback that runs all PostToolUse hooks.

    PostToolUse hooks are purely informational: their output is ignored and
    they cannot modify the tool response. Hook errors are logged but
    do not affect the agent.

    Returns None when there are no PostToolUse hooks.
    """
    post_hooks = [h for h in hooks if h.event == "PostToolUse"]
    if not post_hooks:
        return None

    def after_tool(
        tool: BaseTool,
        args: dict[str, Any],
        tool_context: ToolContext,
        tool_response: dict[str, Any],
    ) -> dict | None:
        tool_name = tool.name
        input_data: dict[str, Any] = {
            "hook_event_name": "PostToolUse",
            "tool_name": tool_name,
            "tool_input": args,
            "tool_response": tool_response,
        }

        for hook in post_hooks:
            if not _matches_tool(hook, tool_name):
                continue
            execute_hook_subprocess(hook, input_data)  # output intentionally ignored

        return None  # never modify the tool response

    return after_tool


async def run_session_hooks(hooks: list[HookConfig], event: str, session_id: str) -> None:
    """Fire all hooks for a session lifecycle event asynchronously.

    Runs each matching hook in the default thread-pool executor so the async
    event loop is not blocked. Errors from individual hooks are logged and
    do not propagate — session hooks are informational.

    Args:
        hooks:      All hooks configured for the agent.
        event:      "SessionStart" or "SessionEnd".
        session_id: The current session identifier.
    """
    session_hooks = [h for h in hooks if h.event == event]
    if not session_hooks:
        return

    input_data: dict[str, Any] = {
        "hook_event_name": event,
        "session_id": session_id,
    }

    loop = asyncio.get_event_loop()
    for hook in session_hooks:
        try:
            await loop.run_in_executor(
                None,
                functools.partial(execute_hook_subprocess, hook, input_data),
            )
        except Exception as exc:
            logger.error("Session hook %r (%s) failed: %s", hook.command, event, exc)
