import a2a_pb2 as _a2a_pb2
from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ListSessionsInput(_message.Message):
    __slots__ = ("page_size", "page_token")
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    page_size: int
    page_token: str
    def __init__(self, page_size: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class SessionSummary(_message.Message):
    __slots__ = ("id", "agent", "state")
    ID_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    id: str
    agent: str
    state: str
    def __init__(self, id: _Optional[str] = ..., agent: _Optional[str] = ..., state: _Optional[str] = ...) -> None: ...

class ListSessionsOutput(_message.Message):
    __slots__ = ("sessions", "next_page_token")
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[SessionSummary]
    next_page_token: str
    def __init__(self, sessions: _Optional[_Iterable[_Union[SessionSummary, _Mapping]]] = ..., next_page_token: _Optional[str] = ...) -> None: ...

class InvokeSessionInput(_message.Message):
    __slots__ = ("session_id", "message", "message_id")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    message: str
    message_id: str
    def __init__(self, session_id: _Optional[str] = ..., message: _Optional[str] = ..., message_id: _Optional[str] = ...) -> None: ...

class InvokeSessionOutput(_message.Message):
    __slots__ = ("session_id", "task_id", "context_id", "state", "text")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    task_id: str
    context_id: str
    state: _a2a_pb2.TaskState
    text: str
    def __init__(self, session_id: _Optional[str] = ..., task_id: _Optional[str] = ..., context_id: _Optional[str] = ..., state: _Optional[_Union[_a2a_pb2.TaskState, str]] = ..., text: _Optional[str] = ...) -> None: ...
