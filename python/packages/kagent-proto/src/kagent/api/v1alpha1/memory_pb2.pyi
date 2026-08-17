import datetime

from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SessionMemoryInput(_message.Message):
    __slots__ = ("agent_name", "user_id", "content", "vector", "metadata", "ttl_days")
    AGENT_NAME_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    VECTOR_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    TTL_DAYS_FIELD_NUMBER: _ClassVar[int]
    agent_name: str
    user_id: str
    content: str
    vector: _containers.RepeatedScalarFieldContainer[float]
    metadata: _struct_pb2.Struct
    ttl_days: int
    def __init__(self, agent_name: _Optional[str] = ..., user_id: _Optional[str] = ..., content: _Optional[str] = ..., vector: _Optional[_Iterable[float]] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., ttl_days: _Optional[int] = ...) -> None: ...

class MemorySearchResult(_message.Message):
    __slots__ = ("id", "content", "score", "metadata", "created_at")
    ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    SCORE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    content: str
    score: float
    metadata: _struct_pb2.Struct
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., content: _Optional[str] = ..., score: _Optional[float] = ..., metadata: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class MemorySummary(_message.Message):
    __slots__ = ("id", "content", "access_count", "created_at", "expires_at")
    ID_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    ACCESS_COUNT_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    content: str
    access_count: int
    created_at: _timestamp_pb2.Timestamp
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., content: _Optional[str] = ..., access_count: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class MemoryServiceAddSessionRequest(_message.Message):
    __slots__ = ("memory",)
    MEMORY_FIELD_NUMBER: _ClassVar[int]
    memory: SessionMemoryInput
    def __init__(self, memory: _Optional[_Union[SessionMemoryInput, _Mapping]] = ...) -> None: ...

class MemoryServiceAddSessionResponse(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class MemoryServiceAddSessionBatchRequest(_message.Message):
    __slots__ = ("items",)
    ITEMS_FIELD_NUMBER: _ClassVar[int]
    items: _containers.RepeatedCompositeFieldContainer[SessionMemoryInput]
    def __init__(self, items: _Optional[_Iterable[_Union[SessionMemoryInput, _Mapping]]] = ...) -> None: ...

class MemoryServiceAddSessionBatchResponse(_message.Message):
    __slots__ = ("count",)
    COUNT_FIELD_NUMBER: _ClassVar[int]
    count: int
    def __init__(self, count: _Optional[int] = ...) -> None: ...

class MemoryServiceSearchRequest(_message.Message):
    __slots__ = ("agent_name", "user_id", "vector", "limit", "min_score")
    AGENT_NAME_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    VECTOR_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    MIN_SCORE_FIELD_NUMBER: _ClassVar[int]
    agent_name: str
    user_id: str
    vector: _containers.RepeatedScalarFieldContainer[float]
    limit: int
    min_score: float
    def __init__(self, agent_name: _Optional[str] = ..., user_id: _Optional[str] = ..., vector: _Optional[_Iterable[float]] = ..., limit: _Optional[int] = ..., min_score: _Optional[float] = ...) -> None: ...

class MemoryServiceSearchResponse(_message.Message):
    __slots__ = ("memories",)
    MEMORIES_FIELD_NUMBER: _ClassVar[int]
    memories: _containers.RepeatedCompositeFieldContainer[MemorySearchResult]
    def __init__(self, memories: _Optional[_Iterable[_Union[MemorySearchResult, _Mapping]]] = ...) -> None: ...

class MemoryServiceListRequest(_message.Message):
    __slots__ = ("agent_name", "user_id")
    AGENT_NAME_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    agent_name: str
    user_id: str
    def __init__(self, agent_name: _Optional[str] = ..., user_id: _Optional[str] = ...) -> None: ...

class MemoryServiceListResponse(_message.Message):
    __slots__ = ("memories",)
    MEMORIES_FIELD_NUMBER: _ClassVar[int]
    memories: _containers.RepeatedCompositeFieldContainer[MemorySummary]
    def __init__(self, memories: _Optional[_Iterable[_Union[MemorySummary, _Mapping]]] = ...) -> None: ...

class MemoryServiceDeleteRequest(_message.Message):
    __slots__ = ("agent_name", "user_id")
    AGENT_NAME_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    agent_name: str
    user_id: str
    def __init__(self, agent_name: _Optional[str] = ..., user_id: _Optional[str] = ...) -> None: ...

class MemoryServiceDeleteResponse(_message.Message):
    __slots__ = ("status",)
    STATUS_FIELD_NUMBER: _ClassVar[int]
    status: str
    def __init__(self, status: _Optional[str] = ...) -> None: ...
