---
name: Global Commenting
description: Write minimal, helpful comments following the self-documenting code philosophy where clear structure and naming are prioritized over extensive commenting in the Kagent codebase. Use this skill when writing Go package documentation comments (// Package httpserver provides...), when writing Go function documentation explaining what the function does and its behavior, when adding strategic inline comments to explain complex algorithms or business logic (not obvious code), when writing Python module docstrings describing the module's purpose and functionality, when writing Python class docstrings explaining what the class does and how it integrates with the system, when writing Python function docstrings in Google style with Args/Returns/Raises sections for public API functions, when documenting TypeScript/React components with JSDoc comments (/** Component description */), when documenting React component props interfaces with inline comments for each prop (/** The agent to chat with */), when adding comments to explain non-obvious decisions or trade-offs in the code (why, not what), when adding TODO or FIXME comments to mark incomplete work with context about what needs to be done, when explaining complex logic like priority ordering or state transitions with inline comments, when documenting API/interface contracts explaining return codes and error conditions, when adding warnings about gotchas or unexpected behavior (WARNING: This mutates the input), when avoiding comments that simply restate what the code does or explain obvious variable names, when avoiding comments about temporary changes or tied to specific bug fixes or dates, when avoiding commenting out dead code (delete it instead, it's in git history), when keeping comments concise and evergreen (relevant far into the future), or when ensuring comments stay updated when the code changes (outdated comments are worse than no comments). This skill ensures comments add value by explaining complex logic, business rules, and non-obvious decisions rather than restating what well-written code already communicates through clear naming and structure.
---

# Global Commenting

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle global commenting.

## Instructions

For details, refer to the information provided in this file:
[global commenting](../../../agent-os/standards/global/commenting.md)
