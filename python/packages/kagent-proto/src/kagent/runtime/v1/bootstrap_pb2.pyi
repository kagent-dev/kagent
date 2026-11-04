from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class BootstrapServiceGetBootstrapConfigRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class BootstrapServiceGetBootstrapConfigResponse(_message.Message):
    __slots__ = ("format_version", "revision", "config_json", "agent_card_json", "sha256")
    FORMAT_VERSION_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    CONFIG_JSON_FIELD_NUMBER: _ClassVar[int]
    AGENT_CARD_JSON_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    format_version: int
    revision: str
    config_json: bytes
    agent_card_json: bytes
    sha256: str
    def __init__(self, format_version: _Optional[int] = ..., revision: _Optional[str] = ..., config_json: _Optional[bytes] = ..., agent_card_json: _Optional[bytes] = ..., sha256: _Optional[str] = ...) -> None: ...
