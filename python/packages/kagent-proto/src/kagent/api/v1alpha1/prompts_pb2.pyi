from kagent.api.v1alpha1 import common_pb2 as _common_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PromptTemplateSummary(_message.Message):
    __slots__ = ("ref", "key_count", "keys")
    REF_FIELD_NUMBER: _ClassVar[int]
    KEY_COUNT_FIELD_NUMBER: _ClassVar[int]
    KEYS_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    key_count: int
    keys: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., key_count: _Optional[int] = ..., keys: _Optional[_Iterable[str]] = ...) -> None: ...

class PromptTemplate(_message.Message):
    __slots__ = ("ref", "data")
    class DataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    REF_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    data: _containers.ScalarMap[str, str]
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., data: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ListPromptTemplatesRequest(_message.Message):
    __slots__ = ("namespace",)
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    def __init__(self, namespace: _Optional[str] = ...) -> None: ...

class ListPromptTemplatesResponse(_message.Message):
    __slots__ = ("prompt_templates",)
    PROMPT_TEMPLATES_FIELD_NUMBER: _ClassVar[int]
    prompt_templates: _containers.RepeatedCompositeFieldContainer[PromptTemplateSummary]
    def __init__(self, prompt_templates: _Optional[_Iterable[_Union[PromptTemplateSummary, _Mapping]]] = ...) -> None: ...

class GetPromptTemplateRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class GetPromptTemplateResponse(_message.Message):
    __slots__ = ("prompt_template",)
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    prompt_template: PromptTemplate
    def __init__(self, prompt_template: _Optional[_Union[PromptTemplate, _Mapping]] = ...) -> None: ...

class CreatePromptTemplateRequest(_message.Message):
    __slots__ = ("ref", "data")
    class DataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    REF_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    data: _containers.ScalarMap[str, str]
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., data: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreatePromptTemplateResponse(_message.Message):
    __slots__ = ("prompt_template",)
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    prompt_template: PromptTemplate
    def __init__(self, prompt_template: _Optional[_Union[PromptTemplate, _Mapping]] = ...) -> None: ...

class UpdatePromptTemplateRequest(_message.Message):
    __slots__ = ("ref", "data")
    class DataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    REF_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    data: _containers.ScalarMap[str, str]
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., data: _Optional[_Mapping[str, str]] = ...) -> None: ...

class UpdatePromptTemplateResponse(_message.Message):
    __slots__ = ("prompt_template",)
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    prompt_template: PromptTemplate
    def __init__(self, prompt_template: _Optional[_Union[PromptTemplate, _Mapping]] = ...) -> None: ...

class DeletePromptTemplateRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class DeletePromptTemplateResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
