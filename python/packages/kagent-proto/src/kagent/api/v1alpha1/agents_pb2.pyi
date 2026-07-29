from kagent.api.v1alpha1 import common_pb2 as _common_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AgentKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_KIND_UNSPECIFIED: _ClassVar[AgentKind]
    AGENT_KIND_AGENT: _ClassVar[AgentKind]
    AGENT_KIND_SANDBOX_AGENT: _ClassVar[AgentKind]
    AGENT_KIND_AGENT_HARNESS: _ClassVar[AgentKind]

class WorkloadMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKLOAD_MODE_UNSPECIFIED: _ClassVar[WorkloadMode]
    WORKLOAD_MODE_DEPLOYMENT: _ClassVar[WorkloadMode]
    WORKLOAD_MODE_SANDBOX: _ClassVar[WorkloadMode]

class AgentHarnessActorState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_HARNESS_ACTOR_STATE_UNSPECIFIED: _ClassVar[AgentHarnessActorState]
    AGENT_HARNESS_ACTOR_STATE_RUNNING: _ClassVar[AgentHarnessActorState]
    AGENT_HARNESS_ACTOR_STATE_SUSPENDED: _ClassVar[AgentHarnessActorState]
    AGENT_HARNESS_ACTOR_STATE_MISSING: _ClassVar[AgentHarnessActorState]
AGENT_KIND_UNSPECIFIED: AgentKind
AGENT_KIND_AGENT: AgentKind
AGENT_KIND_SANDBOX_AGENT: AgentKind
AGENT_KIND_AGENT_HARNESS: AgentKind
WORKLOAD_MODE_UNSPECIFIED: WorkloadMode
WORKLOAD_MODE_DEPLOYMENT: WorkloadMode
WORKLOAD_MODE_SANDBOX: WorkloadMode
AGENT_HARNESS_ACTOR_STATE_UNSPECIFIED: AgentHarnessActorState
AGENT_HARNESS_ACTOR_STATE_RUNNING: AgentHarnessActorState
AGENT_HARNESS_ACTOR_STATE_SUSPENDED: AgentHarnessActorState
AGENT_HARNESS_ACTOR_STATE_MISSING: AgentHarnessActorState

class AgentHarnessDetails(_message.Message):
    __slots__ = ("backend", "actor_id", "backend_ref_id", "endpoint", "acp_path")
    BACKEND_FIELD_NUMBER: _ClassVar[int]
    ACTOR_ID_FIELD_NUMBER: _ClassVar[int]
    BACKEND_REF_ID_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    ACP_PATH_FIELD_NUMBER: _ClassVar[int]
    backend: str
    actor_id: str
    backend_ref_id: str
    endpoint: str
    acp_path: str
    def __init__(self, backend: _Optional[str] = ..., actor_id: _Optional[str] = ..., backend_ref_id: _Optional[str] = ..., endpoint: _Optional[str] = ..., acp_path: _Optional[str] = ...) -> None: ...

class Agent(_message.Message):
    __slots__ = ("ref", "kind", "resource", "id", "model_provider", "model", "model_config_ref", "tools", "deployment_ready", "accepted", "workload_mode", "agent_harness", "memory_refs")
    REF_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    ID_FIELD_NUMBER: _ClassVar[int]
    MODEL_PROVIDER_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    MODEL_CONFIG_REF_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    DEPLOYMENT_READY_FIELD_NUMBER: _ClassVar[int]
    ACCEPTED_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_MODE_FIELD_NUMBER: _ClassVar[int]
    AGENT_HARNESS_FIELD_NUMBER: _ClassVar[int]
    MEMORY_REFS_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    kind: AgentKind
    resource: _common_pb2.StructuredObject
    id: str
    model_provider: str
    model: str
    model_config_ref: _common_pb2.ResourceReference
    tools: _containers.RepeatedCompositeFieldContainer[_common_pb2.StructuredObject]
    deployment_ready: bool
    accepted: bool
    workload_mode: WorkloadMode
    agent_harness: AgentHarnessDetails
    memory_refs: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., kind: _Optional[_Union[AgentKind, str]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ..., id: _Optional[str] = ..., model_provider: _Optional[str] = ..., model: _Optional[str] = ..., model_config_ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., tools: _Optional[_Iterable[_Union[_common_pb2.StructuredObject, _Mapping]]] = ..., deployment_ready: _Optional[bool] = ..., accepted: _Optional[bool] = ..., workload_mode: _Optional[_Union[WorkloadMode, str]] = ..., agent_harness: _Optional[_Union[AgentHarnessDetails, _Mapping]] = ..., memory_refs: _Optional[_Iterable[str]] = ...) -> None: ...

class ListAgentsRequest(_message.Message):
    __slots__ = ("namespace",)
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    def __init__(self, namespace: _Optional[str] = ...) -> None: ...

class ListAgentsResponse(_message.Message):
    __slots__ = ("agents",)
    AGENTS_FIELD_NUMBER: _ClassVar[int]
    agents: _containers.RepeatedCompositeFieldContainer[Agent]
    def __init__(self, agents: _Optional[_Iterable[_Union[Agent, _Mapping]]] = ...) -> None: ...

class GetAgentRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class GetAgentResponse(_message.Message):
    __slots__ = ("agent",)
    AGENT_FIELD_NUMBER: _ClassVar[int]
    agent: Agent
    def __init__(self, agent: _Optional[_Union[Agent, _Mapping]] = ...) -> None: ...

class CreateAgentRequest(_message.Message):
    __slots__ = ("ref", "resource")
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class CreateAgentResponse(_message.Message):
    __slots__ = ("agent",)
    AGENT_FIELD_NUMBER: _ClassVar[int]
    agent: Agent
    def __init__(self, agent: _Optional[_Union[Agent, _Mapping]] = ...) -> None: ...

class UpdateAgentRequest(_message.Message):
    __slots__ = ("ref", "resource")
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class UpdateAgentResponse(_message.Message):
    __slots__ = ("agent",)
    AGENT_FIELD_NUMBER: _ClassVar[int]
    agent: Agent
    def __init__(self, agent: _Optional[_Union[Agent, _Mapping]] = ...) -> None: ...

class DeleteAgentRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class DeleteAgentResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetSandboxAgentRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class GetSandboxAgentResponse(_message.Message):
    __slots__ = ("agent",)
    AGENT_FIELD_NUMBER: _ClassVar[int]
    agent: Agent
    def __init__(self, agent: _Optional[_Union[Agent, _Mapping]] = ...) -> None: ...

class CreateSandboxAgentRequest(_message.Message):
    __slots__ = ("ref", "resource")
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class CreateSandboxAgentResponse(_message.Message):
    __slots__ = ("agent",)
    AGENT_FIELD_NUMBER: _ClassVar[int]
    agent: Agent
    def __init__(self, agent: _Optional[_Union[Agent, _Mapping]] = ...) -> None: ...

class UpdateSandboxAgentRequest(_message.Message):
    __slots__ = ("ref", "resource")
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class UpdateSandboxAgentResponse(_message.Message):
    __slots__ = ("agent",)
    AGENT_FIELD_NUMBER: _ClassVar[int]
    agent: Agent
    def __init__(self, agent: _Optional[_Union[Agent, _Mapping]] = ...) -> None: ...

class DeleteSandboxAgentRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class DeleteSandboxAgentResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetAgentHarnessRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class GetAgentHarnessResponse(_message.Message):
    __slots__ = ("agent",)
    AGENT_FIELD_NUMBER: _ClassVar[int]
    agent: Agent
    def __init__(self, agent: _Optional[_Union[Agent, _Mapping]] = ...) -> None: ...

class CreateAgentHarnessRequest(_message.Message):
    __slots__ = ("ref", "resource")
    REF_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    resource: _common_pb2.StructuredObject
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., resource: _Optional[_Union[_common_pb2.StructuredObject, _Mapping]] = ...) -> None: ...

class CreateAgentHarnessResponse(_message.Message):
    __slots__ = ("agent",)
    AGENT_FIELD_NUMBER: _ClassVar[int]
    agent: Agent
    def __init__(self, agent: _Optional[_Union[Agent, _Mapping]] = ...) -> None: ...

class DeleteAgentHarnessRequest(_message.Message):
    __slots__ = ("ref",)
    REF_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ...) -> None: ...

class DeleteAgentHarnessResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class EnsureAgentHarnessSessionActorRequest(_message.Message):
    __slots__ = ("ref", "session_id")
    REF_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    session_id: str
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., session_id: _Optional[str] = ...) -> None: ...

class SuspendAgentHarnessSessionActorRequest(_message.Message):
    __slots__ = ("ref", "session_id")
    REF_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    session_id: str
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., session_id: _Optional[str] = ...) -> None: ...

class GetAgentHarnessSessionActorRequest(_message.Message):
    __slots__ = ("ref", "session_id")
    REF_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    session_id: str
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., session_id: _Optional[str] = ...) -> None: ...

class AgentHarnessSessionActor(_message.Message):
    __slots__ = ("ref", "session_id", "actor_id", "state")
    REF_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ACTOR_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    ref: _common_pb2.ResourceReference
    session_id: str
    actor_id: str
    state: AgentHarnessActorState
    def __init__(self, ref: _Optional[_Union[_common_pb2.ResourceReference, _Mapping]] = ..., session_id: _Optional[str] = ..., actor_id: _Optional[str] = ..., state: _Optional[_Union[AgentHarnessActorState, str]] = ...) -> None: ...

class EnsureAgentHarnessSessionActorResponse(_message.Message):
    __slots__ = ("actor",)
    ACTOR_FIELD_NUMBER: _ClassVar[int]
    actor: AgentHarnessSessionActor
    def __init__(self, actor: _Optional[_Union[AgentHarnessSessionActor, _Mapping]] = ...) -> None: ...

class SuspendAgentHarnessSessionActorResponse(_message.Message):
    __slots__ = ("actor",)
    ACTOR_FIELD_NUMBER: _ClassVar[int]
    actor: AgentHarnessSessionActor
    def __init__(self, actor: _Optional[_Union[AgentHarnessSessionActor, _Mapping]] = ...) -> None: ...

class GetAgentHarnessSessionActorResponse(_message.Message):
    __slots__ = ("actor",)
    ACTOR_FIELD_NUMBER: _ClassVar[int]
    actor: AgentHarnessSessionActor
    def __init__(self, actor: _Optional[_Union[AgentHarnessSessionActor, _Mapping]] = ...) -> None: ...
