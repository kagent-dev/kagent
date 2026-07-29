from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class LangGraphCheckpoint(_message.Message):
    __slots__ = ("thread_id", "checkpoint_ns", "checkpoint_id", "parent_checkpoint_id", "checkpoint", "metadata", "type", "version")
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_NS_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    PARENT_CHECKPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    checkpoint_ns: str
    checkpoint_id: str
    parent_checkpoint_id: str
    checkpoint: bytes
    metadata: bytes
    type: str
    version: int
    def __init__(self, thread_id: _Optional[str] = ..., checkpoint_ns: _Optional[str] = ..., checkpoint_id: _Optional[str] = ..., parent_checkpoint_id: _Optional[str] = ..., checkpoint: _Optional[bytes] = ..., metadata: _Optional[bytes] = ..., type: _Optional[str] = ..., version: _Optional[int] = ...) -> None: ...

class LangGraphCheckpointWrite(_message.Message):
    __slots__ = ("idx", "channel", "type", "value", "task_id")
    IDX_FIELD_NUMBER: _ClassVar[int]
    CHANNEL_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    idx: int
    channel: str
    type: str
    value: bytes
    task_id: str
    def __init__(self, idx: _Optional[int] = ..., channel: _Optional[str] = ..., type: _Optional[str] = ..., value: _Optional[bytes] = ..., task_id: _Optional[str] = ...) -> None: ...

class LangGraphCheckpointWrites(_message.Message):
    __slots__ = ("thread_id", "checkpoint_ns", "checkpoint_id", "task_id", "writes")
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_NS_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    WRITES_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    checkpoint_ns: str
    checkpoint_id: str
    task_id: str
    writes: _containers.RepeatedCompositeFieldContainer[LangGraphCheckpointWrite]
    def __init__(self, thread_id: _Optional[str] = ..., checkpoint_ns: _Optional[str] = ..., checkpoint_id: _Optional[str] = ..., task_id: _Optional[str] = ..., writes: _Optional[_Iterable[_Union[LangGraphCheckpointWrite, _Mapping]]] = ...) -> None: ...

class LangGraphCheckpointTuple(_message.Message):
    __slots__ = ("checkpoint", "writes")
    CHECKPOINT_FIELD_NUMBER: _ClassVar[int]
    WRITES_FIELD_NUMBER: _ClassVar[int]
    checkpoint: LangGraphCheckpoint
    writes: LangGraphCheckpointWrites
    def __init__(self, checkpoint: _Optional[_Union[LangGraphCheckpoint, _Mapping]] = ..., writes: _Optional[_Union[LangGraphCheckpointWrites, _Mapping]] = ...) -> None: ...

class PutCheckpointRequest(_message.Message):
    __slots__ = ("checkpoint",)
    CHECKPOINT_FIELD_NUMBER: _ClassVar[int]
    checkpoint: LangGraphCheckpoint
    def __init__(self, checkpoint: _Optional[_Union[LangGraphCheckpoint, _Mapping]] = ...) -> None: ...

class PutCheckpointResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListCheckpointsRequest(_message.Message):
    __slots__ = ("thread_id", "checkpoint_ns", "checkpoint_id", "limit")
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_NS_FIELD_NUMBER: _ClassVar[int]
    CHECKPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    checkpoint_ns: str
    checkpoint_id: str
    limit: int
    def __init__(self, thread_id: _Optional[str] = ..., checkpoint_ns: _Optional[str] = ..., checkpoint_id: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class ListCheckpointsResponse(_message.Message):
    __slots__ = ("checkpoints",)
    CHECKPOINTS_FIELD_NUMBER: _ClassVar[int]
    checkpoints: _containers.RepeatedCompositeFieldContainer[LangGraphCheckpointTuple]
    def __init__(self, checkpoints: _Optional[_Iterable[_Union[LangGraphCheckpointTuple, _Mapping]]] = ...) -> None: ...

class PutWritesRequest(_message.Message):
    __slots__ = ("writes",)
    WRITES_FIELD_NUMBER: _ClassVar[int]
    writes: LangGraphCheckpointWrites
    def __init__(self, writes: _Optional[_Union[LangGraphCheckpointWrites, _Mapping]] = ...) -> None: ...

class PutWritesResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class DeleteThreadRequest(_message.Message):
    __slots__ = ("thread_id",)
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    def __init__(self, thread_id: _Optional[str] = ...) -> None: ...

class DeleteThreadResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
