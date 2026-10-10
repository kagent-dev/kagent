"""KAgent OpenAI Integration Package.

This package provides OpenAI integrations for KAgent.
"""

from kagent.core.telemetry import _defaults as _defaults

# isort: split

# Re-export from agent subpackage for convenience
from ._a2a import KAgentApp
from .tools import get_skill_tools

__all__ = ["KAgentApp", "get_skill_tools"]
__version__ = "0.1.0"
