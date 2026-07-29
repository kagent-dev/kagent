from google.protobuf import struct_pb2 as _struct_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class GetVersionRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetVersionResponse(_message.Message):
    __slots__ = ("kagent_version", "git_commit", "build_date")
    KAGENT_VERSION_FIELD_NUMBER: _ClassVar[int]
    GIT_COMMIT_FIELD_NUMBER: _ClassVar[int]
    BUILD_DATE_FIELD_NUMBER: _ClassVar[int]
    kagent_version: str
    git_commit: str
    build_date: str
    def __init__(self, kagent_version: _Optional[str] = ..., git_commit: _Optional[str] = ..., build_date: _Optional[str] = ...) -> None: ...

class GetCurrentUserRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetCurrentUserResponse(_message.Message):
    __slots__ = ("claims",)
    CLAIMS_FIELD_NUMBER: _ClassVar[int]
    claims: _struct_pb2.Struct
    def __init__(self, claims: _Optional[_Union[_struct_pb2.Struct, _Mapping]] = ...) -> None: ...

class ListNamespacesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class Namespace(_message.Message):
    __slots__ = ("name", "status")
    NAME_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    name: str
    status: str
    def __init__(self, name: _Optional[str] = ..., status: _Optional[str] = ...) -> None: ...

class ListNamespacesResponse(_message.Message):
    __slots__ = ("namespaces",)
    NAMESPACES_FIELD_NUMBER: _ClassVar[int]
    namespaces: _containers.RepeatedCompositeFieldContainer[Namespace]
    def __init__(self, namespaces: _Optional[_Iterable[_Union[Namespace, _Mapping]]] = ...) -> None: ...

class GetSubstrateStatusRequest(_message.Message):
    __slots__ = ("namespace",)
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    def __init__(self, namespace: _Optional[str] = ...) -> None: ...

class GetSubstrateStatusResponse(_message.Message):
    __slots__ = ("enabled", "ate_api_error", "worker_pools", "actor_templates", "actors", "workers")
    ENABLED_FIELD_NUMBER: _ClassVar[int]
    ATE_API_ERROR_FIELD_NUMBER: _ClassVar[int]
    WORKER_POOLS_FIELD_NUMBER: _ClassVar[int]
    ACTOR_TEMPLATES_FIELD_NUMBER: _ClassVar[int]
    ACTORS_FIELD_NUMBER: _ClassVar[int]
    WORKERS_FIELD_NUMBER: _ClassVar[int]
    enabled: bool
    ate_api_error: str
    worker_pools: _containers.RepeatedCompositeFieldContainer[SubstrateWorkerPool]
    actor_templates: _containers.RepeatedCompositeFieldContainer[SubstrateActorTemplate]
    actors: _containers.RepeatedCompositeFieldContainer[SubstrateActor]
    workers: _containers.RepeatedCompositeFieldContainer[SubstrateWorker]
    def __init__(self, enabled: _Optional[bool] = ..., ate_api_error: _Optional[str] = ..., worker_pools: _Optional[_Iterable[_Union[SubstrateWorkerPool, _Mapping]]] = ..., actor_templates: _Optional[_Iterable[_Union[SubstrateActorTemplate, _Mapping]]] = ..., actors: _Optional[_Iterable[_Union[SubstrateActor, _Mapping]]] = ..., workers: _Optional[_Iterable[_Union[SubstrateWorker, _Mapping]]] = ...) -> None: ...

class SubstrateWorkerPool(_message.Message):
    __slots__ = ("namespace", "name", "replicas", "ateom_image")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    REPLICAS_FIELD_NUMBER: _ClassVar[int]
    ATEOM_IMAGE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    replicas: int
    ateom_image: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., replicas: _Optional[int] = ..., ateom_image: _Optional[str] = ...) -> None: ...

class SubstrateActorTemplate(_message.Message):
    __slots__ = ("namespace", "name", "phase", "golden_actor_id", "golden_snapshot", "sandbox_class", "worker_selector", "harness_name", "managed_by_kagent")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    GOLDEN_ACTOR_ID_FIELD_NUMBER: _ClassVar[int]
    GOLDEN_SNAPSHOT_FIELD_NUMBER: _ClassVar[int]
    SANDBOX_CLASS_FIELD_NUMBER: _ClassVar[int]
    WORKER_SELECTOR_FIELD_NUMBER: _ClassVar[int]
    HARNESS_NAME_FIELD_NUMBER: _ClassVar[int]
    MANAGED_BY_KAGENT_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    phase: str
    golden_actor_id: str
    golden_snapshot: str
    sandbox_class: str
    worker_selector: str
    harness_name: str
    managed_by_kagent: bool
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., phase: _Optional[str] = ..., golden_actor_id: _Optional[str] = ..., golden_snapshot: _Optional[str] = ..., sandbox_class: _Optional[str] = ..., worker_selector: _Optional[str] = ..., harness_name: _Optional[str] = ..., managed_by_kagent: _Optional[bool] = ...) -> None: ...

class SubstrateActor(_message.Message):
    __slots__ = ("actor_id", "atespace", "status", "actor_template_namespace", "actor_template_name", "ateom_pod_namespace", "ateom_pod_name", "ateom_pod_ip", "latest_snapshot", "worker_pool_name", "in_progress_snapshot", "version")
    ACTOR_ID_FIELD_NUMBER: _ClassVar[int]
    ATESPACE_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    ACTOR_TEMPLATE_NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ACTOR_TEMPLATE_NAME_FIELD_NUMBER: _ClassVar[int]
    ATEOM_POD_NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ATEOM_POD_NAME_FIELD_NUMBER: _ClassVar[int]
    ATEOM_POD_IP_FIELD_NUMBER: _ClassVar[int]
    LATEST_SNAPSHOT_FIELD_NUMBER: _ClassVar[int]
    WORKER_POOL_NAME_FIELD_NUMBER: _ClassVar[int]
    IN_PROGRESS_SNAPSHOT_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    actor_id: str
    atespace: str
    status: str
    actor_template_namespace: str
    actor_template_name: str
    ateom_pod_namespace: str
    ateom_pod_name: str
    ateom_pod_ip: str
    latest_snapshot: str
    worker_pool_name: str
    in_progress_snapshot: str
    version: int
    def __init__(self, actor_id: _Optional[str] = ..., atespace: _Optional[str] = ..., status: _Optional[str] = ..., actor_template_namespace: _Optional[str] = ..., actor_template_name: _Optional[str] = ..., ateom_pod_namespace: _Optional[str] = ..., ateom_pod_name: _Optional[str] = ..., ateom_pod_ip: _Optional[str] = ..., latest_snapshot: _Optional[str] = ..., worker_pool_name: _Optional[str] = ..., in_progress_snapshot: _Optional[str] = ..., version: _Optional[int] = ...) -> None: ...

class SubstrateWorker(_message.Message):
    __slots__ = ("worker_namespace", "worker_pool", "worker_pod", "actor_namespace", "actor_template", "actor_id", "ip", "version")
    WORKER_NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    WORKER_POOL_FIELD_NUMBER: _ClassVar[int]
    WORKER_POD_FIELD_NUMBER: _ClassVar[int]
    ACTOR_NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ACTOR_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    ACTOR_ID_FIELD_NUMBER: _ClassVar[int]
    IP_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    worker_namespace: str
    worker_pool: str
    worker_pod: str
    actor_namespace: str
    actor_template: str
    actor_id: str
    ip: str
    version: int
    def __init__(self, worker_namespace: _Optional[str] = ..., worker_pool: _Optional[str] = ..., worker_pod: _Optional[str] = ..., actor_namespace: _Optional[str] = ..., actor_template: _Optional[str] = ..., actor_id: _Optional[str] = ..., ip: _Optional[str] = ..., version: _Optional[int] = ...) -> None: ...
