import importlib.metadata

from .a2a import KAgentApp
from .models import AgentConfig

__version__ = importlib.metadata.version("kagent_adk")

__all__ = ["KAgentApp", "AgentConfig"]
