import importlib.metadata
import warnings

# Suppress repeated experimental mode warnings from google-adk's A2A decorators.
# Without this filter, every RemoteA2aAgent/A2aAgentExecutor instantiation emits
# a UserWarning, flooding logs during normal A2A operations.
# See: https://github.com/kagent-dev/kagent/issues/1379
warnings.filterwarnings(
    "once",
    message=r"\[EXPERIMENTAL\].*(RemoteA2aAgent|A2aAgentExecutor)",
    category=UserWarning,
)

from ._a2a import KAgentApp  # noqa: E402
from .types import AgentConfig  # noqa: E402

__version__ = importlib.metadata.version("kagent_adk")

__all__ = ["KAgentApp", "AgentConfig"]
