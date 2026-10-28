from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ToolActivity(_message.Message):
    __slots__ = ("id", "name", "args", "response")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    ARGS_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    args: _struct_pb2.Value
    response: ToolResponse
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., args: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., response: _Optional[_Union[ToolResponse, _Mapping]] = ...) -> None: ...

class ToolResponse(_message.Message):
    __slots__ = ("output", "error", "pending")
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    PENDING_FIELD_NUMBER: _ClassVar[int]
    output: _struct_pb2.Value
    error: _struct_pb2.Value
    pending: _struct_pb2.Value
    def __init__(self, output: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., error: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ..., pending: _Optional[_Union[_struct_pb2.Value, _Mapping]] = ...) -> None: ...

class HitlTool(_message.Message):
    __slots__ = ("id", "call_id", "name", "args")
    ID_FIELD_NUMBER: _ClassVar[int]
    CALL_ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    ARGS_FIELD_NUMBER: _ClassVar[int]
    id: str
    call_id: str
    name: str
    args: _struct_pb2.Struct
    def __init__(self, id: _Optional[str] = ..., call_id: _Optional[str] = ..., name: _Optional[str] = ..., args: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class NestedHitlRequest(_message.Message):
    __slots__ = ("subagent_name", "task_id", "context_id", "tools")
    SUBAGENT_NAME_FIELD_NUMBER: _ClassVar[int]
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_ID_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    subagent_name: str
    task_id: str
    context_id: str
    tools: _containers.RepeatedCompositeFieldContainer[HitlTool]
    def __init__(self, subagent_name: _Optional[str] = ..., task_id: _Optional[str] = ..., context_id: _Optional[str] = ..., tools: _Optional[_Iterable[_Union[HitlTool, _Mapping]]] = ...) -> None: ...

class ToolApprovalRequest(_message.Message):
    __slots__ = ("type", "hint", "tools", "nested")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    HINT_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    NESTED_FIELD_NUMBER: _ClassVar[int]
    type: str
    hint: str
    tools: _containers.RepeatedCompositeFieldContainer[HitlTool]
    nested: NestedHitlRequest
    def __init__(self, type: _Optional[str] = ..., hint: _Optional[str] = ..., tools: _Optional[_Iterable[_Union[HitlTool, _Mapping]]] = ..., nested: _Optional[_Union[NestedHitlRequest, _Mapping]] = ...) -> None: ...

class ToolApproval(_message.Message):
    __slots__ = ("id", "approved", "rejection_reason")
    ID_FIELD_NUMBER: _ClassVar[int]
    APPROVED_FIELD_NUMBER: _ClassVar[int]
    REJECTION_REASON_FIELD_NUMBER: _ClassVar[int]
    id: str
    approved: bool
    rejection_reason: str
    def __init__(self, id: _Optional[str] = ..., approved: _Optional[bool] = ..., rejection_reason: _Optional[str] = ...) -> None: ...

class ToolApprovalResponse(_message.Message):
    __slots__ = ("type", "approvals")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    APPROVALS_FIELD_NUMBER: _ClassVar[int]
    type: str
    approvals: _containers.RepeatedCompositeFieldContainer[ToolApproval]
    def __init__(self, type: _Optional[str] = ..., approvals: _Optional[_Iterable[_Union[ToolApproval, _Mapping]]] = ...) -> None: ...

class HitlQuestion(_message.Message):
    __slots__ = ("question", "choices", "multiple")
    QUESTION_FIELD_NUMBER: _ClassVar[int]
    CHOICES_FIELD_NUMBER: _ClassVar[int]
    MULTIPLE_FIELD_NUMBER: _ClassVar[int]
    question: str
    choices: _containers.RepeatedScalarFieldContainer[str]
    multiple: bool
    def __init__(self, question: _Optional[str] = ..., choices: _Optional[_Iterable[str]] = ..., multiple: _Optional[bool] = ...) -> None: ...

class AskUserRequest(_message.Message):
    __slots__ = ("type", "id", "questions", "nested")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    QUESTIONS_FIELD_NUMBER: _ClassVar[int]
    NESTED_FIELD_NUMBER: _ClassVar[int]
    type: str
    id: str
    questions: _containers.RepeatedCompositeFieldContainer[HitlQuestion]
    nested: NestedHitlRequest
    def __init__(self, type: _Optional[str] = ..., id: _Optional[str] = ..., questions: _Optional[_Iterable[_Union[HitlQuestion, _Mapping]]] = ..., nested: _Optional[_Union[NestedHitlRequest, _Mapping]] = ...) -> None: ...

class AskUserAnswer(_message.Message):
    __slots__ = ("answer",)
    ANSWER_FIELD_NUMBER: _ClassVar[int]
    answer: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, answer: _Optional[_Iterable[str]] = ...) -> None: ...

class AskUserResponse(_message.Message):
    __slots__ = ("type", "id", "answers")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    ANSWERS_FIELD_NUMBER: _ClassVar[int]
    type: str
    id: str
    answers: _containers.RepeatedCompositeFieldContainer[AskUserAnswer]
    def __init__(self, type: _Optional[str] = ..., id: _Optional[str] = ..., answers: _Optional[_Iterable[_Union[AskUserAnswer, _Mapping]]] = ...) -> None: ...
