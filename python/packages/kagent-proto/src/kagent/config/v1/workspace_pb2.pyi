from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional

DESCRIPTOR: _descriptor.FileDescriptor

class GitWorkspaceConfig(_message.Message):
    __slots__ = ("url", "ref", "authorization", "refresh_token")
    URL_FIELD_NUMBER: _ClassVar[int]
    REF_FIELD_NUMBER: _ClassVar[int]
    AUTHORIZATION_FIELD_NUMBER: _ClassVar[int]
    REFRESH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    url: str
    ref: str
    authorization: bool
    refresh_token: str
    def __init__(self, url: _Optional[str] = ..., ref: _Optional[str] = ..., authorization: _Optional[bool] = ..., refresh_token: _Optional[str] = ...) -> None: ...
