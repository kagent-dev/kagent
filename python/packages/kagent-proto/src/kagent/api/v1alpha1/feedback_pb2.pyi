import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Feedback(_message.Message):
    __slots__ = ("id", "created_at", "updated_at", "deleted_at", "user_id", "message_id", "is_positive", "feedback_text", "issue_type")
    ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    DELETED_AT_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    IS_POSITIVE_FIELD_NUMBER: _ClassVar[int]
    FEEDBACK_TEXT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_TYPE_FIELD_NUMBER: _ClassVar[int]
    id: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    deleted_at: _timestamp_pb2.Timestamp
    user_id: str
    message_id: int
    is_positive: bool
    feedback_text: str
    issue_type: str
    def __init__(self, id: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., deleted_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., user_id: _Optional[str] = ..., message_id: _Optional[int] = ..., is_positive: _Optional[bool] = ..., feedback_text: _Optional[str] = ..., issue_type: _Optional[str] = ...) -> None: ...

class CreateFeedbackRequest(_message.Message):
    __slots__ = ("message_id", "is_positive", "feedback_text", "issue_type")
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    IS_POSITIVE_FIELD_NUMBER: _ClassVar[int]
    FEEDBACK_TEXT_FIELD_NUMBER: _ClassVar[int]
    ISSUE_TYPE_FIELD_NUMBER: _ClassVar[int]
    message_id: int
    is_positive: bool
    feedback_text: str
    issue_type: str
    def __init__(self, message_id: _Optional[int] = ..., is_positive: _Optional[bool] = ..., feedback_text: _Optional[str] = ..., issue_type: _Optional[str] = ...) -> None: ...

class CreateFeedbackResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListFeedbackRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListFeedbackResponse(_message.Message):
    __slots__ = ("feedback",)
    FEEDBACK_FIELD_NUMBER: _ClassVar[int]
    feedback: _containers.RepeatedCompositeFieldContainer[Feedback]
    def __init__(self, feedback: _Optional[_Iterable[_Union[Feedback, _Mapping]]] = ...) -> None: ...
