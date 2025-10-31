"""KAgent OpenAI Agents SDK Integration Package.

This package provides OpenAI Agents SDK integration for KAgent with A2A server support.
It includes:
- KAgentApp: FastAPI application builder for deploying OpenAI agents
- Session management via KAgent backend
- Event streaming for agent execution
- Skills support (in agents.skills subpackage)
- File operation tools (in agents.tools subpackage)
"""

from ._a2a import KAgentApp

__all__ = ["KAgentApp"]
__version__ = "0.1.0"
