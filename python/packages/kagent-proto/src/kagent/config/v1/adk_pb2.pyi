import datetime

from buf.validate import validate_pb2 as _validate_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from kagent.config.v1 import config_pb2 as _config_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ADKConfig(_message.Message):
    __slots__ = ("runtime", "agent")
    RUNTIME_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    runtime: ADKRuntimeConfig
    agent: ADKAgentConfig
    def __init__(self, runtime: _Optional[_Union[ADKRuntimeConfig, _Mapping]] = ..., agent: _Optional[_Union[ADKAgentConfig, _Mapping]] = ...) -> None: ...

class ADKAgentConfig(_message.Message):
    __slots__ = ("name", "model", "description", "instruction", "http_tools", "sse_tools", "stdio_tools", "remote_agents", "stream", "memory", "network", "agent_plugins", "context_config", "share_tools", "session_db_url", "skills_directory", "sub_agents", "output")
    NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    INSTRUCTION_FIELD_NUMBER: _ClassVar[int]
    HTTP_TOOLS_FIELD_NUMBER: _ClassVar[int]
    SSE_TOOLS_FIELD_NUMBER: _ClassVar[int]
    STDIO_TOOLS_FIELD_NUMBER: _ClassVar[int]
    REMOTE_AGENTS_FIELD_NUMBER: _ClassVar[int]
    STREAM_FIELD_NUMBER: _ClassVar[int]
    MEMORY_FIELD_NUMBER: _ClassVar[int]
    NETWORK_FIELD_NUMBER: _ClassVar[int]
    AGENT_PLUGINS_FIELD_NUMBER: _ClassVar[int]
    CONTEXT_CONFIG_FIELD_NUMBER: _ClassVar[int]
    SHARE_TOOLS_FIELD_NUMBER: _ClassVar[int]
    SESSION_DB_URL_FIELD_NUMBER: _ClassVar[int]
    SKILLS_DIRECTORY_FIELD_NUMBER: _ClassVar[int]
    SUB_AGENTS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_FIELD_NUMBER: _ClassVar[int]
    name: str
    model: ADKModelConfig
    description: str
    instruction: str
    http_tools: _containers.RepeatedCompositeFieldContainer[ADKHTTPMCPServerConfig]
    sse_tools: _containers.RepeatedCompositeFieldContainer[ADKSSEMCPServerConfig]
    stdio_tools: _containers.RepeatedCompositeFieldContainer[ADKStdioMCPServerConfig]
    remote_agents: _containers.RepeatedCompositeFieldContainer[ADKRemoteAgentConfig]
    stream: bool
    memory: ADKMemoryConfig
    network: ADKNetworkConfig
    agent_plugins: ADKAgentPlugins
    context_config: ADKContextConfig
    share_tools: bool
    session_db_url: str
    skills_directory: str
    sub_agents: _containers.RepeatedCompositeFieldContainer[ADKAgentConfig]
    output: ADKOutputConfig
    def __init__(self, name: _Optional[str] = ..., model: _Optional[_Union[ADKModelConfig, _Mapping]] = ..., description: _Optional[str] = ..., instruction: _Optional[str] = ..., http_tools: _Optional[_Iterable[_Union[ADKHTTPMCPServerConfig, _Mapping]]] = ..., sse_tools: _Optional[_Iterable[_Union[ADKSSEMCPServerConfig, _Mapping]]] = ..., stdio_tools: _Optional[_Iterable[_Union[ADKStdioMCPServerConfig, _Mapping]]] = ..., remote_agents: _Optional[_Iterable[_Union[ADKRemoteAgentConfig, _Mapping]]] = ..., stream: _Optional[bool] = ..., memory: _Optional[_Union[ADKMemoryConfig, _Mapping]] = ..., network: _Optional[_Union[ADKNetworkConfig, _Mapping]] = ..., agent_plugins: _Optional[_Union[ADKAgentPlugins, _Mapping]] = ..., context_config: _Optional[_Union[ADKContextConfig, _Mapping]] = ..., share_tools: _Optional[bool] = ..., session_db_url: _Optional[str] = ..., skills_directory: _Optional[str] = ..., sub_agents: _Optional[_Iterable[_Union[ADKAgentConfig, _Mapping]]] = ..., output: _Optional[_Union[ADKOutputConfig, _Mapping]] = ...) -> None: ...

class InvocationCredentialHeader(_message.Message):
    __slots__ = ("name", "prefix")
    NAME_FIELD_NUMBER: _ClassVar[int]
    PREFIX_FIELD_NUMBER: _ClassVar[int]
    name: str
    prefix: str
    def __init__(self, name: _Optional[str] = ..., prefix: _Optional[str] = ...) -> None: ...

class ADKModelConfig(_message.Message):
    __slots__ = ("model", "headers", "tls_insecure_skip_verify", "tls_ca_cert_path", "tls_disable_system_cas", "api_key_passthrough", "passthrough_headers", "openai", "azure_openai", "anthropic", "gemini_vertex_ai", "gemini_anthropic", "ollama", "gemini", "bedrock", "sap_ai_core", "foundry", "mistral")
    class HeadersEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    MODEL_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    TLS_INSECURE_SKIP_VERIFY_FIELD_NUMBER: _ClassVar[int]
    TLS_CA_CERT_PATH_FIELD_NUMBER: _ClassVar[int]
    TLS_DISABLE_SYSTEM_CAS_FIELD_NUMBER: _ClassVar[int]
    API_KEY_PASSTHROUGH_FIELD_NUMBER: _ClassVar[int]
    PASSTHROUGH_HEADERS_FIELD_NUMBER: _ClassVar[int]
    OPENAI_FIELD_NUMBER: _ClassVar[int]
    AZURE_OPENAI_FIELD_NUMBER: _ClassVar[int]
    ANTHROPIC_FIELD_NUMBER: _ClassVar[int]
    GEMINI_VERTEX_AI_FIELD_NUMBER: _ClassVar[int]
    GEMINI_ANTHROPIC_FIELD_NUMBER: _ClassVar[int]
    OLLAMA_FIELD_NUMBER: _ClassVar[int]
    GEMINI_FIELD_NUMBER: _ClassVar[int]
    BEDROCK_FIELD_NUMBER: _ClassVar[int]
    SAP_AI_CORE_FIELD_NUMBER: _ClassVar[int]
    FOUNDRY_FIELD_NUMBER: _ClassVar[int]
    MISTRAL_FIELD_NUMBER: _ClassVar[int]
    model: str
    headers: _containers.ScalarMap[str, str]
    tls_insecure_skip_verify: bool
    tls_ca_cert_path: str
    tls_disable_system_cas: bool
    api_key_passthrough: bool
    passthrough_headers: _containers.RepeatedCompositeFieldContainer[InvocationCredentialHeader]
    openai: ADKOpenAI
    azure_openai: ADKAzureOpenAI
    anthropic: ADKAnthropic
    gemini_vertex_ai: ADKGeminiVertexAI
    gemini_anthropic: ADKGeminiAnthropic
    ollama: ADKOllama
    gemini: ADKGemini
    bedrock: ADKBedrock
    sap_ai_core: ADKSAPAICore
    foundry: ADKFoundry
    mistral: ADKMistral
    def __init__(self, model: _Optional[str] = ..., headers: _Optional[_Mapping[str, str]] = ..., tls_insecure_skip_verify: _Optional[bool] = ..., tls_ca_cert_path: _Optional[str] = ..., tls_disable_system_cas: _Optional[bool] = ..., api_key_passthrough: _Optional[bool] = ..., passthrough_headers: _Optional[_Iterable[_Union[InvocationCredentialHeader, _Mapping]]] = ..., openai: _Optional[_Union[ADKOpenAI, _Mapping]] = ..., azure_openai: _Optional[_Union[ADKAzureOpenAI, _Mapping]] = ..., anthropic: _Optional[_Union[ADKAnthropic, _Mapping]] = ..., gemini_vertex_ai: _Optional[_Union[ADKGeminiVertexAI, _Mapping]] = ..., gemini_anthropic: _Optional[_Union[ADKGeminiAnthropic, _Mapping]] = ..., ollama: _Optional[_Union[ADKOllama, _Mapping]] = ..., gemini: _Optional[_Union[ADKGemini, _Mapping]] = ..., bedrock: _Optional[_Union[ADKBedrock, _Mapping]] = ..., sap_ai_core: _Optional[_Union[ADKSAPAICore, _Mapping]] = ..., foundry: _Optional[_Union[ADKFoundry, _Mapping]] = ..., mistral: _Optional[_Union[ADKMistral, _Mapping]] = ...) -> None: ...

class ADKOpenAI(_message.Message):
    __slots__ = ("base_url", "frequency_penalty", "max_tokens", "max_completion_tokens", "n", "presence_penalty", "reasoning_effort", "seed", "temperature", "timeout", "top_p", "api_format", "token_exchange", "strict_output_schema")
    BASE_URL_FIELD_NUMBER: _ClassVar[int]
    FREQUENCY_PENALTY_FIELD_NUMBER: _ClassVar[int]
    MAX_TOKENS_FIELD_NUMBER: _ClassVar[int]
    MAX_COMPLETION_TOKENS_FIELD_NUMBER: _ClassVar[int]
    N_FIELD_NUMBER: _ClassVar[int]
    PRESENCE_PENALTY_FIELD_NUMBER: _ClassVar[int]
    REASONING_EFFORT_FIELD_NUMBER: _ClassVar[int]
    SEED_FIELD_NUMBER: _ClassVar[int]
    TEMPERATURE_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    TOP_P_FIELD_NUMBER: _ClassVar[int]
    API_FORMAT_FIELD_NUMBER: _ClassVar[int]
    TOKEN_EXCHANGE_FIELD_NUMBER: _ClassVar[int]
    STRICT_OUTPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    base_url: str
    frequency_penalty: float
    max_tokens: int
    max_completion_tokens: int
    n: int
    presence_penalty: float
    reasoning_effort: str
    seed: int
    temperature: float
    timeout: _duration_pb2.Duration
    top_p: float
    api_format: str
    token_exchange: ADKTokenExchangeConfig
    strict_output_schema: bool
    def __init__(self, base_url: _Optional[str] = ..., frequency_penalty: _Optional[float] = ..., max_tokens: _Optional[int] = ..., max_completion_tokens: _Optional[int] = ..., n: _Optional[int] = ..., presence_penalty: _Optional[float] = ..., reasoning_effort: _Optional[str] = ..., seed: _Optional[int] = ..., temperature: _Optional[float] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., top_p: _Optional[float] = ..., api_format: _Optional[str] = ..., token_exchange: _Optional[_Union[ADKTokenExchangeConfig, _Mapping]] = ..., strict_output_schema: _Optional[bool] = ...) -> None: ...

class ADKTokenExchangeConfig(_message.Message):
    __slots__ = ("gdch_service_account",)
    GDCH_SERVICE_ACCOUNT_FIELD_NUMBER: _ClassVar[int]
    gdch_service_account: ADKGDCHTokenExchangeConfig
    def __init__(self, gdch_service_account: _Optional[_Union[ADKGDCHTokenExchangeConfig, _Mapping]] = ...) -> None: ...

class ADKGDCHTokenExchangeConfig(_message.Message):
    __slots__ = ("service_account_path", "audience")
    SERVICE_ACCOUNT_PATH_FIELD_NUMBER: _ClassVar[int]
    AUDIENCE_FIELD_NUMBER: _ClassVar[int]
    service_account_path: str
    audience: str
    def __init__(self, service_account_path: _Optional[str] = ..., audience: _Optional[str] = ...) -> None: ...

class ADKAzureOpenAI(_message.Message):
    __slots__ = ("endpoint", "deployment", "api_version", "max_tokens", "temperature", "top_p", "api_format", "strict_output_schema")
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    DEPLOYMENT_FIELD_NUMBER: _ClassVar[int]
    API_VERSION_FIELD_NUMBER: _ClassVar[int]
    MAX_TOKENS_FIELD_NUMBER: _ClassVar[int]
    TEMPERATURE_FIELD_NUMBER: _ClassVar[int]
    TOP_P_FIELD_NUMBER: _ClassVar[int]
    API_FORMAT_FIELD_NUMBER: _ClassVar[int]
    STRICT_OUTPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    endpoint: str
    deployment: str
    api_version: str
    max_tokens: int
    temperature: float
    top_p: float
    api_format: str
    strict_output_schema: bool
    def __init__(self, endpoint: _Optional[str] = ..., deployment: _Optional[str] = ..., api_version: _Optional[str] = ..., max_tokens: _Optional[int] = ..., temperature: _Optional[float] = ..., top_p: _Optional[float] = ..., api_format: _Optional[str] = ..., strict_output_schema: _Optional[bool] = ...) -> None: ...

class ADKAnthropic(_message.Message):
    __slots__ = ("base_url", "max_tokens", "temperature", "top_p", "top_k", "timeout", "prompt_caching", "cache_ttl")
    BASE_URL_FIELD_NUMBER: _ClassVar[int]
    MAX_TOKENS_FIELD_NUMBER: _ClassVar[int]
    TEMPERATURE_FIELD_NUMBER: _ClassVar[int]
    TOP_P_FIELD_NUMBER: _ClassVar[int]
    TOP_K_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    PROMPT_CACHING_FIELD_NUMBER: _ClassVar[int]
    CACHE_TTL_FIELD_NUMBER: _ClassVar[int]
    base_url: str
    max_tokens: int
    temperature: float
    top_p: float
    top_k: int
    timeout: _duration_pb2.Duration
    prompt_caching: bool
    cache_ttl: str
    def __init__(self, base_url: _Optional[str] = ..., max_tokens: _Optional[int] = ..., temperature: _Optional[float] = ..., top_p: _Optional[float] = ..., top_k: _Optional[int] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., prompt_caching: _Optional[bool] = ..., cache_ttl: _Optional[str] = ...) -> None: ...

class ADKMistral(_message.Message):
    __slots__ = ("base_url", "max_tokens", "temperature", "top_p", "timeout")
    BASE_URL_FIELD_NUMBER: _ClassVar[int]
    MAX_TOKENS_FIELD_NUMBER: _ClassVar[int]
    TEMPERATURE_FIELD_NUMBER: _ClassVar[int]
    TOP_P_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    base_url: str
    max_tokens: int
    temperature: float
    top_p: float
    timeout: _duration_pb2.Duration
    def __init__(self, base_url: _Optional[str] = ..., max_tokens: _Optional[int] = ..., temperature: _Optional[float] = ..., top_p: _Optional[float] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class ADKGeminiVertexAI(_message.Message):
    __slots__ = ("max_output_tokens",)
    MAX_OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    max_output_tokens: int
    def __init__(self, max_output_tokens: _Optional[int] = ...) -> None: ...

class ADKGeminiAnthropic(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ADKOllama(_message.Message):
    __slots__ = ("options",)
    class OptionsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    options: _containers.ScalarMap[str, str]
    def __init__(self, options: _Optional[_Mapping[str, str]] = ...) -> None: ...

class ADKGemini(_message.Message):
    __slots__ = ("max_output_tokens",)
    MAX_OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    max_output_tokens: int
    def __init__(self, max_output_tokens: _Optional[int] = ...) -> None: ...

class ADKBedrock(_message.Message):
    __slots__ = ("region", "additional_model_request_fields", "prompt_caching", "cache_ttl", "guardrail", "read_timeout", "connect_timeout")
    REGION_FIELD_NUMBER: _ClassVar[int]
    ADDITIONAL_MODEL_REQUEST_FIELDS_FIELD_NUMBER: _ClassVar[int]
    PROMPT_CACHING_FIELD_NUMBER: _ClassVar[int]
    CACHE_TTL_FIELD_NUMBER: _ClassVar[int]
    GUARDRAIL_FIELD_NUMBER: _ClassVar[int]
    READ_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    CONNECT_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    region: str
    additional_model_request_fields: bytes
    prompt_caching: bool
    cache_ttl: str
    guardrail: ADKBedrockGuardrail
    read_timeout: _duration_pb2.Duration
    connect_timeout: _duration_pb2.Duration
    def __init__(self, region: _Optional[str] = ..., additional_model_request_fields: _Optional[bytes] = ..., prompt_caching: _Optional[bool] = ..., cache_ttl: _Optional[str] = ..., guardrail: _Optional[_Union[ADKBedrockGuardrail, _Mapping]] = ..., read_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., connect_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class ADKBedrockGuardrail(_message.Message):
    __slots__ = ("identifier", "version", "trace")
    IDENTIFIER_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    TRACE_FIELD_NUMBER: _ClassVar[int]
    identifier: str
    version: str
    trace: str
    def __init__(self, identifier: _Optional[str] = ..., version: _Optional[str] = ..., trace: _Optional[str] = ...) -> None: ...

class ADKSAPAICore(_message.Message):
    __slots__ = ("base_url", "resource_group", "auth_url")
    BASE_URL_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_GROUP_FIELD_NUMBER: _ClassVar[int]
    AUTH_URL_FIELD_NUMBER: _ClassVar[int]
    base_url: str
    resource_group: str
    auth_url: str
    def __init__(self, base_url: _Optional[str] = ..., resource_group: _Optional[str] = ..., auth_url: _Optional[str] = ...) -> None: ...

class ADKFoundry(_message.Message):
    __slots__ = ("endpoint", "deployment", "api_version", "api_format", "strict_output_schema")
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    DEPLOYMENT_FIELD_NUMBER: _ClassVar[int]
    API_VERSION_FIELD_NUMBER: _ClassVar[int]
    API_FORMAT_FIELD_NUMBER: _ClassVar[int]
    STRICT_OUTPUT_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    endpoint: str
    deployment: str
    api_version: str
    api_format: str
    strict_output_schema: bool
    def __init__(self, endpoint: _Optional[str] = ..., deployment: _Optional[str] = ..., api_version: _Optional[str] = ..., api_format: _Optional[str] = ..., strict_output_schema: _Optional[bool] = ...) -> None: ...

class ADKHTTPConnectionParams(_message.Message):
    __slots__ = ("url", "headers", "timeout", "sse_read_timeout", "terminate_on_close", "tls_insecure_skip_verify", "tls_ca_cert_path", "tls_disable_system_cas")
    class HeadersEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    URL_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    SSE_READ_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    TERMINATE_ON_CLOSE_FIELD_NUMBER: _ClassVar[int]
    TLS_INSECURE_SKIP_VERIFY_FIELD_NUMBER: _ClassVar[int]
    TLS_CA_CERT_PATH_FIELD_NUMBER: _ClassVar[int]
    TLS_DISABLE_SYSTEM_CAS_FIELD_NUMBER: _ClassVar[int]
    url: str
    headers: _containers.ScalarMap[str, str]
    timeout: _duration_pb2.Duration
    sse_read_timeout: _duration_pb2.Duration
    terminate_on_close: bool
    tls_insecure_skip_verify: bool
    tls_ca_cert_path: str
    tls_disable_system_cas: bool
    def __init__(self, url: _Optional[str] = ..., headers: _Optional[_Mapping[str, str]] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., sse_read_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., terminate_on_close: _Optional[bool] = ..., tls_insecure_skip_verify: _Optional[bool] = ..., tls_ca_cert_path: _Optional[str] = ..., tls_disable_system_cas: _Optional[bool] = ...) -> None: ...

class ADKSSEConnectionParams(_message.Message):
    __slots__ = ("url", "headers", "timeout", "sse_read_timeout", "tls_insecure_skip_verify", "tls_ca_cert_path", "tls_disable_system_cas")
    class HeadersEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    URL_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    SSE_READ_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    TLS_INSECURE_SKIP_VERIFY_FIELD_NUMBER: _ClassVar[int]
    TLS_CA_CERT_PATH_FIELD_NUMBER: _ClassVar[int]
    TLS_DISABLE_SYSTEM_CAS_FIELD_NUMBER: _ClassVar[int]
    url: str
    headers: _containers.ScalarMap[str, str]
    timeout: _duration_pb2.Duration
    sse_read_timeout: _duration_pb2.Duration
    tls_insecure_skip_verify: bool
    tls_ca_cert_path: str
    tls_disable_system_cas: bool
    def __init__(self, url: _Optional[str] = ..., headers: _Optional[_Mapping[str, str]] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., sse_read_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., tls_insecure_skip_verify: _Optional[bool] = ..., tls_ca_cert_path: _Optional[str] = ..., tls_disable_system_cas: _Optional[bool] = ...) -> None: ...

class ADKHTTPMCPServerConfig(_message.Message):
    __slots__ = ("params", "tools", "allowed_headers", "require_approval", "token_passthrough", "passthrough_headers")
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    ALLOWED_HEADERS_FIELD_NUMBER: _ClassVar[int]
    REQUIRE_APPROVAL_FIELD_NUMBER: _ClassVar[int]
    TOKEN_PASSTHROUGH_FIELD_NUMBER: _ClassVar[int]
    PASSTHROUGH_HEADERS_FIELD_NUMBER: _ClassVar[int]
    params: ADKHTTPConnectionParams
    tools: _containers.RepeatedScalarFieldContainer[str]
    allowed_headers: _containers.RepeatedScalarFieldContainer[str]
    require_approval: bool
    token_passthrough: bool
    passthrough_headers: _containers.RepeatedCompositeFieldContainer[InvocationCredentialHeader]
    def __init__(self, params: _Optional[_Union[ADKHTTPConnectionParams, _Mapping]] = ..., tools: _Optional[_Iterable[str]] = ..., allowed_headers: _Optional[_Iterable[str]] = ..., require_approval: _Optional[bool] = ..., token_passthrough: _Optional[bool] = ..., passthrough_headers: _Optional[_Iterable[_Union[InvocationCredentialHeader, _Mapping]]] = ...) -> None: ...

class ADKSSEMCPServerConfig(_message.Message):
    __slots__ = ("params", "tools", "allowed_headers", "require_approval", "token_passthrough", "passthrough_headers")
    PARAMS_FIELD_NUMBER: _ClassVar[int]
    TOOLS_FIELD_NUMBER: _ClassVar[int]
    ALLOWED_HEADERS_FIELD_NUMBER: _ClassVar[int]
    REQUIRE_APPROVAL_FIELD_NUMBER: _ClassVar[int]
    TOKEN_PASSTHROUGH_FIELD_NUMBER: _ClassVar[int]
    PASSTHROUGH_HEADERS_FIELD_NUMBER: _ClassVar[int]
    params: ADKSSEConnectionParams
    tools: _containers.RepeatedScalarFieldContainer[str]
    allowed_headers: _containers.RepeatedScalarFieldContainer[str]
    require_approval: bool
    token_passthrough: bool
    passthrough_headers: _containers.RepeatedCompositeFieldContainer[InvocationCredentialHeader]
    def __init__(self, params: _Optional[_Union[ADKSSEConnectionParams, _Mapping]] = ..., tools: _Optional[_Iterable[str]] = ..., allowed_headers: _Optional[_Iterable[str]] = ..., require_approval: _Optional[bool] = ..., token_passthrough: _Optional[bool] = ..., passthrough_headers: _Optional[_Iterable[_Union[InvocationCredentialHeader, _Mapping]]] = ...) -> None: ...

class ADKStdioMCPServerConfig(_message.Message):
    __slots__ = ("command", "args", "env", "dir")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    ARGS_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    DIR_FIELD_NUMBER: _ClassVar[int]
    command: str
    args: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    dir: str
    def __init__(self, command: _Optional[str] = ..., args: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., dir: _Optional[str] = ...) -> None: ...

class ADKRemoteAgentConfig(_message.Message):
    __slots__ = ("name", "url", "headers", "description", "isolate_sessions", "timeout")
    class HeadersEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    URL_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    ISOLATE_SESSIONS_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    name: str
    url: str
    headers: _containers.ScalarMap[str, str]
    description: str
    isolate_sessions: bool
    timeout: _duration_pb2.Duration
    def __init__(self, name: _Optional[str] = ..., url: _Optional[str] = ..., headers: _Optional[_Mapping[str, str]] = ..., description: _Optional[str] = ..., isolate_sessions: _Optional[bool] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class ADKEmbeddingConfig(_message.Message):
    __slots__ = ("provider", "model", "base_url", "api_key_passthrough", "tls_insecure_skip_verify", "tls_ca_cert_path", "tls_disable_system_cas", "endpoint", "deployment", "api_version", "headers", "passthrough_headers")
    class HeadersEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    BASE_URL_FIELD_NUMBER: _ClassVar[int]
    API_KEY_PASSTHROUGH_FIELD_NUMBER: _ClassVar[int]
    TLS_INSECURE_SKIP_VERIFY_FIELD_NUMBER: _ClassVar[int]
    TLS_CA_CERT_PATH_FIELD_NUMBER: _ClassVar[int]
    TLS_DISABLE_SYSTEM_CAS_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    DEPLOYMENT_FIELD_NUMBER: _ClassVar[int]
    API_VERSION_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    PASSTHROUGH_HEADERS_FIELD_NUMBER: _ClassVar[int]
    provider: str
    model: str
    base_url: str
    api_key_passthrough: bool
    tls_insecure_skip_verify: bool
    tls_ca_cert_path: str
    tls_disable_system_cas: bool
    endpoint: str
    deployment: str
    api_version: str
    headers: _containers.ScalarMap[str, str]
    passthrough_headers: _containers.RepeatedCompositeFieldContainer[InvocationCredentialHeader]
    def __init__(self, provider: _Optional[str] = ..., model: _Optional[str] = ..., base_url: _Optional[str] = ..., api_key_passthrough: _Optional[bool] = ..., tls_insecure_skip_verify: _Optional[bool] = ..., tls_ca_cert_path: _Optional[str] = ..., tls_disable_system_cas: _Optional[bool] = ..., endpoint: _Optional[str] = ..., deployment: _Optional[str] = ..., api_version: _Optional[str] = ..., headers: _Optional[_Mapping[str, str]] = ..., passthrough_headers: _Optional[_Iterable[_Union[InvocationCredentialHeader, _Mapping]]] = ...) -> None: ...

class ADKMemoryConfig(_message.Message):
    __slots__ = ("ttl_days", "embedding")
    TTL_DAYS_FIELD_NUMBER: _ClassVar[int]
    EMBEDDING_FIELD_NUMBER: _ClassVar[int]
    ttl_days: int
    embedding: ADKEmbeddingConfig
    def __init__(self, ttl_days: _Optional[int] = ..., embedding: _Optional[_Union[ADKEmbeddingConfig, _Mapping]] = ...) -> None: ...

class ADKNetworkConfig(_message.Message):
    __slots__ = ("allowed_domains",)
    ALLOWED_DOMAINS_FIELD_NUMBER: _ClassVar[int]
    allowed_domains: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, allowed_domains: _Optional[_Iterable[str]] = ...) -> None: ...

class ADKContextConfig(_message.Message):
    __slots__ = ("compaction",)
    COMPACTION_FIELD_NUMBER: _ClassVar[int]
    compaction: ADKCompressionConfig
    def __init__(self, compaction: _Optional[_Union[ADKCompressionConfig, _Mapping]] = ...) -> None: ...

class ADKCompressionConfig(_message.Message):
    __slots__ = ("compaction_interval", "overlap_size", "summarizer_model", "prompt_template", "token_threshold", "event_retention_size")
    COMPACTION_INTERVAL_FIELD_NUMBER: _ClassVar[int]
    OVERLAP_SIZE_FIELD_NUMBER: _ClassVar[int]
    SUMMARIZER_MODEL_FIELD_NUMBER: _ClassVar[int]
    PROMPT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    TOKEN_THRESHOLD_FIELD_NUMBER: _ClassVar[int]
    EVENT_RETENTION_SIZE_FIELD_NUMBER: _ClassVar[int]
    compaction_interval: int
    overlap_size: int
    summarizer_model: ADKModelConfig
    prompt_template: str
    token_threshold: int
    event_retention_size: int
    def __init__(self, compaction_interval: _Optional[int] = ..., overlap_size: _Optional[int] = ..., summarizer_model: _Optional[_Union[ADKModelConfig, _Mapping]] = ..., prompt_template: _Optional[str] = ..., token_threshold: _Optional[int] = ..., event_retention_size: _Optional[int] = ...) -> None: ...

class ADKOutputConfig(_message.Message):
    __slots__ = ("json_schema", "sha256")
    JSON_SCHEMA_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    json_schema: bytes
    sha256: str
    def __init__(self, json_schema: _Optional[bytes] = ..., sha256: _Optional[str] = ...) -> None: ...

class ADKAgentPlugins(_message.Message):
    __slots__ = ("skills", "plugins")
    SKILLS_FIELD_NUMBER: _ClassVar[int]
    PLUGINS_FIELD_NUMBER: _ClassVar[int]
    skills: _containers.RepeatedCompositeFieldContainer[ADKSkill]
    plugins: _containers.RepeatedCompositeFieldContainer[ADKPluginBundle]
    def __init__(self, skills: _Optional[_Iterable[_Union[ADKSkill, _Mapping]]] = ..., plugins: _Optional[_Iterable[_Union[ADKPluginBundle, _Mapping]]] = ...) -> None: ...

class ADKSkill(_message.Message):
    __slots__ = ("name", "source")
    NAME_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    name: str
    source: ADKPluginSource
    def __init__(self, name: _Optional[str] = ..., source: _Optional[_Union[ADKPluginSource, _Mapping]] = ...) -> None: ...

class ADKPluginBundle(_message.Message):
    __slots__ = ("source", "skills")
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    SKILLS_FIELD_NUMBER: _ClassVar[int]
    source: ADKPluginSource
    skills: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, source: _Optional[_Union[ADKPluginSource, _Mapping]] = ..., skills: _Optional[_Iterable[str]] = ...) -> None: ...

class ADKPluginSource(_message.Message):
    __slots__ = ("git", "s3", "directory", "path")
    GIT_FIELD_NUMBER: _ClassVar[int]
    S3_FIELD_NUMBER: _ClassVar[int]
    DIRECTORY_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    git: ADKPluginGitSource
    s3: ADKPluginS3Source
    directory: str
    path: str
    def __init__(self, git: _Optional[_Union[ADKPluginGitSource, _Mapping]] = ..., s3: _Optional[_Union[ADKPluginS3Source, _Mapping]] = ..., directory: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class ADKPluginGitSource(_message.Message):
    __slots__ = ("url", "commit")
    URL_FIELD_NUMBER: _ClassVar[int]
    COMMIT_FIELD_NUMBER: _ClassVar[int]
    url: str
    commit: str
    def __init__(self, url: _Optional[str] = ..., commit: _Optional[str] = ...) -> None: ...

class ADKPluginS3Source(_message.Message):
    __slots__ = ("endpoint", "bucket", "key", "version_id", "region")
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    BUCKET_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    VERSION_ID_FIELD_NUMBER: _ClassVar[int]
    REGION_FIELD_NUMBER: _ClassVar[int]
    endpoint: str
    bucket: str
    key: str
    version_id: str
    region: str
    def __init__(self, endpoint: _Optional[str] = ..., bucket: _Optional[str] = ..., key: _Optional[str] = ..., version_id: _Optional[str] = ..., region: _Optional[str] = ...) -> None: ...

class ADKRuntimeConfig(_message.Message):
    __slots__ = ("control_plane_url", "gateway_url", "name", "namespace", "log_level", "host", "port", "max_content_length", "token_propagation", "telemetry", "builtin_tools")
    CONTROL_PLANE_URL_FIELD_NUMBER: _ClassVar[int]
    GATEWAY_URL_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    LOG_LEVEL_FIELD_NUMBER: _ClassVar[int]
    HOST_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    MAX_CONTENT_LENGTH_FIELD_NUMBER: _ClassVar[int]
    TOKEN_PROPAGATION_FIELD_NUMBER: _ClassVar[int]
    TELEMETRY_FIELD_NUMBER: _ClassVar[int]
    BUILTIN_TOOLS_FIELD_NUMBER: _ClassVar[int]
    control_plane_url: str
    gateway_url: str
    name: str
    namespace: str
    log_level: str
    host: str
    port: int
    max_content_length: int
    token_propagation: _config_pb2.TokenPropagationConfig
    telemetry: _config_pb2.TelemetryConfig
    builtin_tools: _config_pb2.BuiltinToolsConfig
    def __init__(self, control_plane_url: _Optional[str] = ..., gateway_url: _Optional[str] = ..., name: _Optional[str] = ..., namespace: _Optional[str] = ..., log_level: _Optional[str] = ..., host: _Optional[str] = ..., port: _Optional[int] = ..., max_content_length: _Optional[int] = ..., token_propagation: _Optional[_Union[_config_pb2.TokenPropagationConfig, _Mapping]] = ..., telemetry: _Optional[_Union[_config_pb2.TelemetryConfig, _Mapping]] = ..., builtin_tools: _Optional[_Union[_config_pb2.BuiltinToolsConfig, _Mapping]] = ...) -> None: ...
