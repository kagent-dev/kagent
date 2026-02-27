import importlib.metadata

from ._a2a import KAgentApp
from .types import AgentConfig

__version__ = importlib.metadata.version("kagent_adk")

__all__ = ["KAgentApp", "AgentConfig"]
