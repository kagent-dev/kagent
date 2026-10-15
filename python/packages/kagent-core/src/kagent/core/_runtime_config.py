"""Load the generated ADK contract using protobuf JSON and shared validation."""

from google.protobuf.json_format import Parse, ParseDict
from google.protobuf.message import Message
from kagent.config.v1.adk_pb2 import ADKRuntimeConfig as RuntimeConfig
from kagent.config.v1.config_pb2 import OTLPConfig, TelemetryConfig, TokenPropagationConfig
from protovalidate import validate


def parse_runtime_config(value: dict | str) -> RuntimeConfig:
    config = Parse(value, RuntimeConfig()) if isinstance(value, str) else ParseDict(value, RuntimeConfig())
    validate(config)
    return config


def optional_field(message: Message, name: str):
    """Preserve protobuf field presence when calling SDKs which use None for defaults."""
    return getattr(message, name) if message.HasField(name) else None
