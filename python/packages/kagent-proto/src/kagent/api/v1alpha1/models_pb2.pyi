from kagent.api.v1alpha1 import common_pb2 as _common_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SecretMaterial(_message.Message):
    __slots__ = ("name", "key", "value")
    NAME_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    name: str
    key: str
    value: str
    def __init__(self, name: _Optional[str] = ..., key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...

class ListModelConfigsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ModelConfig(_message.Message):
    __slots__ = ("ref", "resource")
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class ListModelConfigsResponse(_message.Message):
    __slots__ = ("model_configs",)
    MODEL_CONFIGS_FIELD_NUMBER: _ClassVar[int]
    model_configs: _containers.RepeatedCompositeFieldContainer[ModelConfig]
    def __init__(self, model_configs: _Optional[_Iterable[_Union[ModelConfig, _Mapping]]] = ...) -> None: ...

class GetModelConfigRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class GetModelConfigResponse(_message.Message):
    __slots__ = ("model_config",)
    MODEL_CONFIG_FIELD_NUMBER: _ClassVar[int]
    model_config: ModelConfig
    def __init__(self, model_config: _Optional[_Union[ModelConfig, _Mapping]] = ...) -> None: ...

class CreateModelConfigRequest(_message.Message):
    __slots__ = ("ref", "resource", "api_key", "secrets")
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    API_KEY_FIELD_NUMBER: _ClassVar[int]
    SECRETS_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    api_key: str
    secrets: _containers.RepeatedCompositeFieldContainer[SecretMaterial]
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ..., api_key: _Optional[str] = ..., secrets: _Optional[_Iterable[_Union[SecretMaterial, _Mapping]]] = ...) -> None: ...

class CreateModelConfigResponse(_message.Message):
    __slots__ = ("model_config",)
    MODEL_CONFIG_FIELD_NUMBER: _ClassVar[int]
    model_config: ModelConfig
    def __init__(self, model_config: _Optional[_Union[ModelConfig, _Mapping]] = ...) -> None: ...

class UpdateModelConfigRequest(_message.Message):
    __slots__ = ("ref", "resource", "api_key", "secrets")
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    API_KEY_FIELD_NUMBER: _ClassVar[int]
    SECRETS_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    api_key: str
    secrets: _containers.RepeatedCompositeFieldContainer[SecretMaterial]
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ..., api_key: _Optional[str] = ..., secrets: _Optional[_Iterable[_Union[SecretMaterial, _Mapping]]] = ...) -> None: ...

class UpdateModelConfigResponse(_message.Message):
    __slots__ = ("model_config",)
    MODEL_CONFIG_FIELD_NUMBER: _ClassVar[int]
    model_config: ModelConfig
    def __init__(self, model_config: _Optional[_Union[ModelConfig, _Mapping]] = ...) -> None: ...

class DeleteModelConfigRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class DeleteModelConfigResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ProviderDefinition(_message.Message):
    __slots__ = ("name", "type", "required_params", "optional_params")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_PARAMS_FIELD_NUMBER: _ClassVar[int]
    OPTIONAL_PARAMS_FIELD_NUMBER: _ClassVar[int]
    name: str
    type: str
    required_params: _containers.RepeatedScalarFieldContainer[str]
    optional_params: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, name: _Optional[str] = ..., type: _Optional[str] = ..., required_params: _Optional[_Iterable[str]] = ..., optional_params: _Optional[_Iterable[str]] = ...) -> None: ...

class ListSupportedModelProvidersRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListSupportedModelProvidersResponse(_message.Message):
    __slots__ = ("providers",)
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    providers: _containers.RepeatedCompositeFieldContainer[ProviderDefinition]
    def __init__(self, providers: _Optional[_Iterable[_Union[ProviderDefinition, _Mapping]]] = ...) -> None: ...

class ListSupportedMemoryProvidersRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListSupportedMemoryProvidersResponse(_message.Message):
    __slots__ = ("providers",)
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    providers: _containers.RepeatedCompositeFieldContainer[ProviderDefinition]
    def __init__(self, providers: _Optional[_Iterable[_Union[ProviderDefinition, _Mapping]]] = ...) -> None: ...

class ConfiguredProvider(_message.Message):
    __slots__ = ("name", "type", "endpoint")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    name: str
    type: str
    endpoint: str
    def __init__(self, name: _Optional[str] = ..., type: _Optional[str] = ..., endpoint: _Optional[str] = ...) -> None: ...

class ListConfiguredProvidersRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListConfiguredProvidersResponse(_message.Message):
    __slots__ = ("providers",)
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    providers: _containers.RepeatedCompositeFieldContainer[ConfiguredProvider]
    def __init__(self, providers: _Optional[_Iterable[_Union[ConfiguredProvider, _Mapping]]] = ...) -> None: ...

class ListProviderModelsRequest(_message.Message):
    __slots__ = ("provider_name", "refresh")
    PROVIDER_NAME_FIELD_NUMBER: _ClassVar[int]
    REFRESH_FIELD_NUMBER: _ClassVar[int]
    provider_name: str
    refresh: bool
    def __init__(self, provider_name: _Optional[str] = ..., refresh: _Optional[bool] = ...) -> None: ...

class ListProviderModelsResponse(_message.Message):
    __slots__ = ("provider", "models")
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    MODELS_FIELD_NUMBER: _ClassVar[int]
    provider: str
    models: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, provider: _Optional[str] = ..., models: _Optional[_Iterable[str]] = ...) -> None: ...

class ModelInfo(_message.Message):
    __slots__ = ("name", "function_calling")
    NAME_FIELD_NUMBER: _ClassVar[int]
    FUNCTION_CALLING_FIELD_NUMBER: _ClassVar[int]
    name: str
    function_calling: bool
    def __init__(self, name: _Optional[str] = ..., function_calling: _Optional[bool] = ...) -> None: ...

class ProviderModels(_message.Message):
    __slots__ = ("provider", "models")
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    MODELS_FIELD_NUMBER: _ClassVar[int]
    provider: str
    models: _containers.RepeatedCompositeFieldContainer[ModelInfo]
    def __init__(self, provider: _Optional[str] = ..., models: _Optional[_Iterable[_Union[ModelInfo, _Mapping]]] = ...) -> None: ...

class ListSupportedModelsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListSupportedModelsResponse(_message.Message):
    __slots__ = ("providers",)
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    providers: _containers.RepeatedCompositeFieldContainer[ProviderModels]
    def __init__(self, providers: _Optional[_Iterable[_Union[ProviderModels, _Mapping]]] = ...) -> None: ...
