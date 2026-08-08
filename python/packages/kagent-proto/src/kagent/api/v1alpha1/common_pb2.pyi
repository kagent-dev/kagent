from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StructuredObject(_message.Message):
    __slots__ = ("api_version", "kind", "value")
    API_VERSION_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    api_version: str
    kind: str
    value: _struct_pb2.Struct
    def __init__(self, api_version: _Optional[str] = ..., kind: _Optional[str] = ..., value: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class ResourceReference(_message.Message):
    __slots__ = ("namespace", "name")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class PageRequest(_message.Message):
    __slots__ = ("limit", "page_token")
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    limit: int
    page_token: str
    def __init__(self, limit: _Optional[int] = ..., page_token: _Optional[str] = ...) -> None: ...

class PageResponse(_message.Message):
    __slots__ = ("next_page_token",)
    NEXT_PAGE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    next_page_token: str
    def __init__(self, next_page_token: _Optional[str] = ...) -> None: ...
