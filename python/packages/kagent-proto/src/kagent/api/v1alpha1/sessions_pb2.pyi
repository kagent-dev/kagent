import datetime

import a2a_pb2 as _a2a_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from kagent.api.v1alpha1 import common_pb2 as _common_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SessionSource(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SESSION_SOURCE_UNSPECIFIED: _ClassVar[SessionSource]
    SESSION_SOURCE_USER: _ClassVar[SessionSource]
    SESSION_SOURCE_AGENT: _ClassVar[SessionSource]

class EventOrder(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    EVENT_ORDER_UNSPECIFIED: _ClassVar[EventOrder]
    EVENT_ORDER_ASCENDING: _ClassVar[EventOrder]
    EVENT_ORDER_DESCENDING: _ClassVar[EventOrder]
SESSION_SOURCE_UNSPECIFIED: SessionSource
SESSION_SOURCE_USER: SessionSource
SESSION_SOURCE_AGENT: SessionSource
EVENT_ORDER_UNSPECIFIED: EventOrder
EVENT_ORDER_ASCENDING: EventOrder
EVENT_ORDER_DESCENDING: EventOrder

class Session(_message.Message):
    __slots__ = ("id", "name", "user_id", "created_at", "updated_at", "deleted_at", "agent_id", "source", "share_token", "share_read_only")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    DELETED_AT_FIELD_NUMBER: _ClassVar[int]
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    SHARE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    SHARE_READ_ONLY_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    user_id: str
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    deleted_at: _timestamp_pb2.Timestamp
    agent_id: str
    source: SessionSource
    share_token: str
    share_read_only: bool
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., user_id: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., deleted_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., agent_id: _Optional[str] = ..., source: _Optional[_Union[SessionSource, str]] = ..., share_token: _Optional[str] = ..., share_read_only: _Optional[bool] = ...) -> None: ...

class SessionEvent(_message.Message):
    __slots__ = ("id", "session_id", "user_id", "created_at", "updated_at", "deleted_at", "data")
    ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    DELETED_AT_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    id: str
    session_id: str
    user_id: str
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    deleted_at: _timestamp_pb2.Timestamp
    data: str
    def __init__(self, id: _Optional[str] = ..., session_id: _Optional[str] = ..., user_id: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., deleted_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., data: _Optional[str] = ...) -> None: ...

class SessionShare(_message.Message):
    __slots__ = ("id", "token", "session_id", "user_id", "read_only", "created_at")
    ID_FIELD_NUMBER: _ClassVar[int]
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    READ_ONLY_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: int
    token: str
    session_id: str
    user_id: str
    read_only: bool
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[int] = ..., token: _Optional[str] = ..., session_id: _Optional[str] = ..., user_id: _Optional[str] = ..., read_only: _Optional[bool] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ListSessionsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListSessionsResponse(_message.Message):
    __slots__ = ("sessions",)
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[Session]
    def __init__(self, sessions: _Optional[_Iterable[_Union[Session, _Mapping]]] = ...) -> None: ...

class ListSessionsByAgentRequest(_message.Message):
    __slots__ = ("agent_ref",)
    AGENT_REF_FIELD_NUMBER: _ClassVar[int]
    agent_ref: _common_pb2.ResourceReference
    def __init__(self, agent_ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class ListSessionsByAgentResponse(_message.Message):
    __slots__ = ("sessions",)
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[Session]
    def __init__(self, sessions: _Optional[_Iterable[_Union[Session, _Mapping]]] = ...) -> None: ...

class CreateSessionRequest(_message.Message):
    __slots__ = ("id", "agent_ref", "name", "source")
    ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_REF_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    id: str
    agent_ref: str
    name: str
    source: SessionSource
    def __init__(self, id: _Optional[str] = ..., agent_ref: _Optional[str] = ..., name: _Optional[str] = ..., source: _Optional[_Union[SessionSource, str]] = ...) -> None: ...

class CreateSessionResponse(_message.Message):
    __slots__ = ("session",)
    SESSION_FIELD_NUMBER: _ClassVar[int]
    session: Session
    def __init__(self, session: _Optional[_Union[Session, _Mapping]] = ...) -> None: ...

class GetSessionRequest(_message.Message):
    __slots__ = ("session_id", "order", "after", "limit")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ORDER_FIELD_NUMBER: _ClassVar[int]
    AFTER_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    order: EventOrder
    after: _timestamp_pb2.Timestamp
    limit: int
    def __init__(self, session_id: _Optional[str] = ..., order: _Optional[_Union[EventOrder, str]] = ..., after: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., limit: _Optional[int] = ...) -> None: ...

class GetSessionResponse(_message.Message):
    __slots__ = ("session", "events", "read_only")
    SESSION_FIELD_NUMBER: _ClassVar[int]
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    READ_ONLY_FIELD_NUMBER: _ClassVar[int]
    session: Session
    events: _containers.RepeatedCompositeFieldContainer[SessionEvent]
    read_only: bool
    def __init__(self, session: _Optional[_Union[Session, _Mapping]] = ..., events: _Optional[_Iterable[_Union[SessionEvent, _Mapping]]] = ..., read_only: _Optional[bool] = ...) -> None: ...

class UpdateSessionRequest(_message.Message):
    __slots__ = ("session_id", "name", "agent_ref")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    AGENT_REF_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    name: str
    agent_ref: str
    def __init__(self, session_id: _Optional[str] = ..., name: _Optional[str] = ..., agent_ref: _Optional[str] = ...) -> None: ...

class UpdateSessionResponse(_message.Message):
    __slots__ = ("session",)
    SESSION_FIELD_NUMBER: _ClassVar[int]
    session: Session
    def __init__(self, session: _Optional[_Union[Session, _Mapping]] = ...) -> None: ...

class DeleteSessionRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class DeleteSessionResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class AddSessionEventRequest(_message.Message):
    __slots__ = ("session_id", "id", "data")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    id: str
    data: str
    def __init__(self, session_id: _Optional[str] = ..., id: _Optional[str] = ..., data: _Optional[str] = ...) -> None: ...

class AddSessionEventResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class CreateSessionShareRequest(_message.Message):
    __slots__ = ("session_id", "read_only")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    READ_ONLY_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    read_only: bool
    def __init__(self, session_id: _Optional[str] = ..., read_only: _Optional[bool] = ...) -> None: ...

class CreateSessionShareResponse(_message.Message):
    __slots__ = ("share",)
    SHARE_FIELD_NUMBER: _ClassVar[int]
    share: SessionShare
    def __init__(self, share: _Optional[_Union[SessionShare, _Mapping]] = ...) -> None: ...

class ListSessionSharesRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class ListSessionSharesResponse(_message.Message):
    __slots__ = ("shares",)
    SHARES_FIELD_NUMBER: _ClassVar[int]
    shares: _containers.RepeatedCompositeFieldContainer[SessionShare]
    def __init__(self, shares: _Optional[_Iterable[_Union[SessionShare, _Mapping]]] = ...) -> None: ...

class DeleteSessionShareRequest(_message.Message):
    __slots__ = ("session_id", "token")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    token: str
    def __init__(self, session_id: _Optional[str] = ..., token: _Optional[str] = ...) -> None: ...

class DeleteSessionShareResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class UpsertTaskRequest(_message.Message):
    __slots__ = ("task",)
    TASK_FIELD_NUMBER: _ClassVar[int]
    task: _a2a_pb2.Task
    def __init__(self, task: _Optional[_Union[_a2a_pb2.Task, _Mapping]] = ...) -> None: ...

class UpsertTaskResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetTaskRequest(_message.Message):
    __slots__ = ("task_id",)
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    task_id: str
    def __init__(self, task_id: _Optional[str] = ...) -> None: ...

class GetTaskResponse(_message.Message):
    __slots__ = ("task",)
    TASK_FIELD_NUMBER: _ClassVar[int]
    task: _a2a_pb2.Task
    def __init__(self, task: _Optional[_Union[_a2a_pb2.Task, _Mapping]] = ...) -> None: ...

class DeleteTaskRequest(_message.Message):
    __slots__ = ("task_id",)
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    task_id: str
    def __init__(self, task_id: _Optional[str] = ...) -> None: ...

class DeleteTaskResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListTasksRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class ListTasksResponse(_message.Message):
    __slots__ = ("tasks",)
    TASKS_FIELD_NUMBER: _ClassVar[int]
    tasks: _containers.RepeatedCompositeFieldContainer[_a2a_pb2.Task]
    def __init__(self, tasks: _Optional[_Iterable[_Union[_a2a_pb2.Task, _Mapping]]] = ...) -> None: ...
