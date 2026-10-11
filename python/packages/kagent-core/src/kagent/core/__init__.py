from .telemetry import _defaults

# isort: split

from ._config import KAgentConfig
from ._grpc import AsyncControllerClient, AsyncFileTokenProvider, AsyncTokenProvider
from ._logging import configure_logging
from ._structured_object import decode_structured_object, encode_structured_object
from .telemetry import bootstrap, instrument_app, signal_enabled

configure_logging()

__all__ = [
    "AsyncControllerClient",
    "AsyncFileTokenProvider",
    "AsyncTokenProvider",
    "KAgentConfig",
    "decode_structured_object",
    "encode_structured_object",
    "bootstrap",
    "instrument_app",
    "signal_enabled",
    "configure_logging",
]
