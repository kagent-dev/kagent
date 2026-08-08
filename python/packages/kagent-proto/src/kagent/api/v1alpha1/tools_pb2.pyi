from kagent.api.v1alpha1 import common_pb2 as _common_pb2
from kagent.api.v1alpha1 import models_pb2 as _models_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ListToolsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class Tool(_message.Message):
    __slots__ = ("resource",)
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    resource: _common_pb2.StructuredObject
    def __init__(self, resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class ListToolsResponse(_message.Message):
    __slots__ = ("tools",)
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    tools: _containers.RepeatedCompositeFieldContainer[Tool]
    def __init__(self, tools: _Optional[_Iterable[_Union[Tool, _Mapping]]] = ...) -> None: ...

class ListToolServersRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class DiscoveredTool(_message.Message):
    __slots__ = ("name", "description")
    NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    name: str
    description: str
    def __init__(self, name: _Optional[str] = ..., description: _Optional[str] = ...) -> None: ...

class ToolServer(_message.Message):
    __slots__ = ("ref", "group_kind", "discovered_tools")
    REF_FIELD_NUMBER: _ClassVar[int]
    GROUP_KIND_FIELD_NUMBER: _ClassVar[int]
    DISCOVERED_TOOLS_FIELD_NUMBER: _ClassVar[int]
    ref: str
    group_kind: str
    discovered_tools: _containers.RepeatedCompositeFieldContainer[DiscoveredTool]
    def __init__(self, ref: _Optional[str] = ..., group_kind: _Optional[str] = ..., discovered_tools: _Optional[_Iterable[_Union[DiscoveredTool, _Mapping]]] = ...) -> None: ...

class ListToolServersResponse(_message.Message):
    __slots__ = ("tool_servers",)
    TOOL_SERVERS_FIELD_NUMBER: _ClassVar[int]
    tool_servers: _containers.RepeatedCompositeFieldContainer[ToolServer]
    def __init__(self, tool_servers: _Optional[_Iterable[_Union[ToolServer, _Mapping]]] = ...) -> None: ...

class CreateToolServerRequest(_message.Message):
    __slots__ = ("type", "ref", "resource", "secrets")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    SECRETS_FIELD_NUMBER: _ClassVar[int]
    type: str
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    secrets: _containers.RepeatedCompositeFieldContainer[_models_pb2.SecretMaterial]
    def __init__(self, type: _Optional[str] = ..., ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ..., secrets: _Optional[_Iterable[_Union[_models_pb2.SecretMaterial, _Mapping]]] = ...) -> None: ...

class CreateToolServerResponse(_message.Message):
    __slots__ = ("resource",)
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    resource: _common_pb2.StructuredObject
    def __init__(self, resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class DeleteToolServerRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class DeleteToolServerResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListToolServerTypesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListToolServerTypesResponse(_message.Message):
    __slots__ = ("types",)
    TYPES_FIELD_NUMBER: _ClassVar[int]
    types: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, types: _Optional[_Iterable[str]] = ...) -> None: ...

class MCPServerReference(_message.Message):
    __slots__ = ("ref", "group_kind")
    REF_FIELD_NUMBER: _ClassVar[int]
    GROUP_KIND_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    group_kind: str
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., group_kind: _Optional[str] = ...) -> None: ...

class MCPAppTool(_message.Message):
    __slots__ = ("name", "description", "input_schema", "ui_resource_uri", "meta")
    NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    INPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    UI_RESOURCE_URI_FIELD_NUMBER: _ClassVar[int]
    META_FIELD_NUMBER: _ClassVar[int]
    name: str
    description: str
    input_schema: _common_pb2.StructuredObject
    ui_resource_uri: str
    meta: _common_pb2.StructuredObject
    def __init__(self, name: _Optional[str] = ..., description: _Optional[str] = ..., input_schema: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ..., ui_resource_uri: _Optional[str] = ..., meta: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class ListMCPAppToolsRequest(_message.Message):
    __slots__ = ("server",)
    SERVER_FIELD_NUMBER: _ClassVar[int]
    server: MCPServerReference
    def __init__(self, server: _Optional[_Union[MCPServerReference, _Mapping]] = ...) -> None: ...

class ListMCPAppToolsResponse(_message.Message):
    __slots__ = ("tools",)
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    tools: _containers.RepeatedCompositeFieldContainer[MCPAppTool]
    def __init__(self, tools: _Optional[_Iterable[_Union[MCPAppTool, _Mapping]]] = ...) -> None: ...

class CallMCPAppToolRequest(_message.Message):
    __slots__ = ("server", "tool_name", "arguments")
    SERVER_FIELD_NUMBER: _ClassVar[int]
    TOOL_NAME_FIELD_NUMBER: _ClassVar[int]
    ARGUMENTS_FIELD_NUMBER: _ClassVar[int]
    server: MCPServerReference
    tool_name: str
    arguments: _common_pb2.StructuredObject
    def __init__(self, server: _Optional[_Union[MCPServerReference, _Mapping]] = ..., tool_name: _Optional[str] = ..., arguments: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class CallMCPAppToolResponse(_message.Message):
    __slots__ = ("result",)
    RESULT_FIELD_NUMBER: _ClassVar[int]
    result: _common_pb2.StructuredObject
    def __init__(self, result: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class ReadMCPAppResourceRequest(_message.Message):
    __slots__ = ("server", "uri")
    SERVER_FIELD_NUMBER: _ClassVar[int]
    URI_FIELD_NUMBER: _ClassVar[int]
    server: MCPServerReference
    uri: str
    def __init__(self, server: _Optional[_Union[MCPServerReference, _Mapping]] = ..., uri: _Optional[str] = ...) -> None: ...

class ReadMCPAppResourceResponse(_message.Message):
    __slots__ = ("result",)
    RESULT_FIELD_NUMBER: _ClassVar[int]
    result: _common_pb2.StructuredObject
    def __init__(self, result: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...
