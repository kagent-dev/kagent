import datetime

from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class TelemetryConfig(_message.Message):
    __slots__ = ("traces", "metrics", "logs", "resource_attributes", "capture_content", "capture_raw_api_bodies", "max_capture_bytes", "openai_native_tracing")
    class ResourceAttributesEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    TRACES_FIELD_NUMBER: _ClassVar[int]
    METRICS_FIELD_NUMBER: _ClassVar[int]
    LOGS_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_ATTRIBUTES_FIELD_NUMBER: _ClassVar[int]
    CAPTURE_CONTENT_FIELD_NUMBER: _ClassVar[int]
    CAPTURE_RAW_API_BODIES_FIELD_NUMBER: _ClassVar[int]
    MAX_CAPTURE_BYTES_FIELD_NUMBER: _ClassVar[int]
    OPENAI_NATIVE_TRACING_FIELD_NUMBER: _ClassVar[int]
    traces: OTLPConfig
    metrics: OTLPConfig
    logs: OTLPConfig
    resource_attributes: _containers.ScalarMap[str, str]
    capture_content: bool
    capture_raw_api_bodies: bool
    max_capture_bytes: int
    openai_native_tracing: bool
    def __init__(self, traces: _Optional[_Union[OTLPConfig, _Mapping]] = ..., metrics: _Optional[_Union[OTLPConfig, _Mapping]] = ..., logs: _Optional[_Union[OTLPConfig, _Mapping]] = ..., resource_attributes: _Optional[_Mapping[str, str]] = ..., capture_content: _Optional[bool] = ..., capture_raw_api_bodies: _Optional[bool] = ..., max_capture_bytes: _Optional[int] = ..., openai_native_tracing: _Optional[bool] = ...) -> None: ...

class OTLPConfig(_message.Message):
    __slots__ = ("endpoint", "protocol", "timeout")
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    endpoint: str
    protocol: str
    timeout: _duration_pb2.Duration
    def __init__(self, endpoint: _Optional[str] = ..., protocol: _Optional[str] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class BuiltinToolsConfig(_message.Message):
    __slots__ = ("disable_file_search", "bash_venv_path", "disable_bash")
    DISABLE_FILE_SEARCH_FIELD_NUMBER: _ClassVar[int]
    BASH_VENV_PATH_FIELD_NUMBER: _ClassVar[int]
    DISABLE_BASH_FIELD_NUMBER: _ClassVar[int]
    disable_file_search: bool
    bash_venv_path: str
    disable_bash: bool
    def __init__(self, disable_file_search: _Optional[bool] = ..., bash_venv_path: _Optional[str] = ..., disable_bash: _Optional[bool] = ...) -> None: ...

class TokenPropagationConfig(_message.Message):
    __slots__ = ("enabled", "sts_well_known_uri", "resources", "audiences")
    ENABLED_FIELD_NUMBER: _ClassVar[int]
    STS_WELL_KNOWN_URI_FIELD_NUMBER: _ClassVar[int]
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    AUDIENCES_FIELD_NUMBER: _ClassVar[int]
    enabled: bool
    sts_well_known_uri: str
    resources: _containers.RepeatedScalarFieldContainer[str]
    audiences: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, enabled: _Optional[bool] = ..., sts_well_known_uri: _Optional[str] = ..., resources: _Optional[_Iterable[str]] = ..., audiences: _Optional[_Iterable[str]] = ...) -> None: ...
