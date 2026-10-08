"""KAgent LangGraph Integration Package.

This package provides LangGraph integration for KAgent with A2A server support.
"""

from kagent.core.telemetry import _defaults as _defaults

# isort: split

from ._a2a import KAgentApp
from ._executor import LangGraphAgentExecutor

__all__ = ["KAgentApp", "LangGraphAgentExecutor"]
__version__ = "0.1.0"
