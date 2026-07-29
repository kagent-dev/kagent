from kagent.api.v1alpha1 import common_pb2 as _common_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CrewAIMemory(_message.Message):
    __slots__ = ("thread_id", "user_id", "memory_data")
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    MEMORY_DATA_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    user_id: str
    memory_data: _common_pb2.StructuredObject
    def __init__(self, thread_id: _Optional[str] = ..., user_id: _Optional[str] = ..., memory_data: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class CrewAIFlowState(_message.Message):
    __slots__ = ("thread_id", "method_name", "state_data")
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    METHOD_NAME_FIELD_NUMBER: _ClassVar[int]
    STATE_DATA_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    method_name: str
    state_data: _common_pb2.StructuredObject
    def __init__(self, thread_id: _Optional[str] = ..., method_name: _Optional[str] = ..., state_data: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class StoreMemoryRequest(_message.Message):
    __slots__ = ("thread_id", "memory_data")
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    MEMORY_DATA_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    memory_data: _common_pb2.StructuredObject
    def __init__(self, thread_id: _Optional[str] = ..., memory_data: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class StoreMemoryResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetMemoryRequest(_message.Message):
    __slots__ = ("thread_id", "task_description", "limit")
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    task_description: str
    limit: int
    def __init__(self, thread_id: _Optional[str] = ..., task_description: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class GetMemoryResponse(_message.Message):
    __slots__ = ("memories",)
    MEMORIES_FIELD_NUMBER: _ClassVar[int]
    memories: _containers.RepeatedCompositeFieldContainer[CrewAIMemory]
    def __init__(self, memories: _Optional[_Iterable[_Union[CrewAIMemory, _Mapping]]] = ...) -> None: ...

class ResetMemoryRequest(_message.Message):
    __slots__ = ("thread_id",)
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    def __init__(self, thread_id: _Optional[str] = ...) -> None: ...

class ResetMemoryResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class StoreFlowStateRequest(_message.Message):
    __slots__ = ("thread_id", "method_name", "state_data")
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    METHOD_NAME_FIELD_NUMBER: _ClassVar[int]
    STATE_DATA_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    method_name: str
    state_data: _common_pb2.StructuredObject
    def __init__(self, thread_id: _Optional[str] = ..., method_name: _Optional[str] = ..., state_data: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class StoreFlowStateResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetFlowStateRequest(_message.Message):
    __slots__ = ("thread_id",)
    THREAD_ID_FIELD_NUMBER: _ClassVar[int]
    thread_id: str
    def __init__(self, thread_id: _Optional[str] = ...) -> None: ...

class GetFlowStateResponse(_message.Message):
    __slots__ = ("state",)
    STATE_FIELD_NUMBER: _ClassVar[int]
    state: CrewAIFlowState
    def __init__(self, state: _Optional[_Union[CrewAIFlowState, _Mapping]] = ...) -> None: ...
