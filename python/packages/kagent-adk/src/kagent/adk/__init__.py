import importlib.metadata

from ._a2a import KAgentApp
from ._service_account_service import KAgentServiceAccountService
from ._sts_token_service import KAgentSTSTokenService
from .types import AgentConfig

__version__ = importlib.metadata.version("kagent_adk")

__all__ = [
    "KAgentApp",
    "KAgentSTSTokenService",
    "KAgentServiceAccountService",
    "AgentConfig",
]
