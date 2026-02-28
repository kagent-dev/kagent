import importlib.metadata
import warnings

# Suppress repeated experimental-mode UserWarnings whose messages start with
# "[EXPERIMENTAL]" and mention RemoteA2aAgent or A2aAgentExecutor.  This covers
# warnings from google-adk's A2A decorators, where every instantiation would
# otherwise emit a warning and flood logs during normal A2A operations.
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
