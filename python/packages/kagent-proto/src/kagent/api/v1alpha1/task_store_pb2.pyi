import a2a_pb2 as _a2a_pb2
from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StoredTask(_message.Message):
    __slots__ = ("task", "version")
    TASK_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    task: _a2a_pb2.Task
    version: int
    def __init__(self, task: _Optional[_Union[_a2a_pb2.Task, _Mapping]] = ..., version: _Optional[int] = ...) -> None: ...

class TaskStoreServiceCreateTaskRequest(_message.Message):
    __slots__ = ("agent_instance_id", "task", "dispatch_id")
    AGENT_INSTANCE_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_FIELD_NUMBER: _ClassVar[int]
    DISPATCH_ID_FIELD_NUMBER: _ClassVar[int]
    agent_instance_id: str
    task: _a2a_pb2.Task
    dispatch_id: str
    def __init__(self, agent_instance_id: _Optional[str] = ..., task: _Optional[_Union[_a2a_pb2.Task, _Mapping]] = ..., dispatch_id: _Optional[str] = ...) -> None: ...

class TaskStoreServiceCreateTaskResponse(_message.Message):
    __slots__ = ("version",)
    VERSION_FIELD_NUMBER: _ClassVar[int]
    version: int
    def __init__(self, version: _Optional[int] = ...) -> None: ...

class TaskStoreServiceGetTaskRequest(_message.Message):
    __slots__ = ("agent_instance_id", "task_id")
    AGENT_INSTANCE_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    agent_instance_id: str
    task_id: str
    def __init__(self, agent_instance_id: _Optional[str] = ..., task_id: _Optional[str] = ...) -> None: ...

class TaskStoreServiceGetTaskResponse(_message.Message):
    __slots__ = ("stored",)
    STORED_FIELD_NUMBER: _ClassVar[int]
    stored: StoredTask
    def __init__(self, stored: _Optional[_Union[StoredTask, _Mapping]] = ...) -> None: ...

class TaskStoreServiceUpdateTaskRequest(_message.Message):
    __slots__ = ("agent_instance_id", "task", "expected_version", "event", "dispatch_id")
    AGENT_INSTANCE_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    DISPATCH_ID_FIELD_NUMBER: _ClassVar[int]
    agent_instance_id: str
    task: _a2a_pb2.Task
    expected_version: int
    event: _a2a_pb2.StreamResponse
    dispatch_id: str
    def __init__(self, agent_instance_id: _Optional[str] = ..., task: _Optional[_Union[_a2a_pb2.Task, _Mapping]] = ..., expected_version: _Optional[int] = ..., event: _Optional[_Union[_a2a_pb2.StreamResponse, _Mapping]] = ..., dispatch_id: _Optional[str] = ...) -> None: ...

class TaskStoreServiceUpdateTaskResponse(_message.Message):
    __slots__ = ("version",)
    VERSION_FIELD_NUMBER: _ClassVar[int]
    version: int
    def __init__(self, version: _Optional[int] = ...) -> None: ...

class TaskStoreServiceListTasksRequest(_message.Message):
    __slots__ = ("agent_instance_id", "request")
    AGENT_INSTANCE_ID_FIELD_NUMBER: _ClassVar[int]
    REQUEST_FIELD_NUMBER: _ClassVar[int]
    agent_instance_id: str
    request: _a2a_pb2.ListTasksRequest
    def __init__(self, agent_instance_id: _Optional[str] = ..., request: _Optional[_Union[_a2a_pb2.ListTasksRequest, _Mapping]] = ...) -> None: ...

class TaskStoreServiceListTasksResponse(_message.Message):
    __slots__ = ("result",)
    RESULT_FIELD_NUMBER: _ClassVar[int]
    result: _a2a_pb2.ListTasksResponse
    def __init__(self, result: _Optional[_Union[_a2a_pb2.ListTasksResponse, _Mapping]] = ...) -> None: ...

class TaskStoreServiceSettleTaskRequest(_message.Message):
    __slots__ = ("agent_instance_id", "task_id", "version")
    AGENT_INSTANCE_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    agent_instance_id: str
    task_id: str
    version: int
    def __init__(self, agent_instance_id: _Optional[str] = ..., task_id: _Optional[str] = ..., version: _Optional[int] = ...) -> None: ...

class TaskStoreServiceSettleTaskResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
