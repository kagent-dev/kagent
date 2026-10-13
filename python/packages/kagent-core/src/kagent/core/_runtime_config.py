"""Runtime JSON settings shared with go/api/adk/runtime.go and go/pkg/tracing."""

from typing import Literal
from urllib.parse import urlparse

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


class Settings(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class OTLPConfig(Settings):
    endpoint: str
    protocol: Literal["grpc", "http/protobuf"]
    timeout_ms: int = Field(default=0, ge=0, le=86400000)

    @field_validator("endpoint")
    @classmethod
    def http_endpoint(cls, value: str) -> str:
        parsed = urlparse(value)
        if parsed.scheme not in ("http", "https") or not parsed.hostname:
            raise ValueError("must be an HTTP(S) URL")
        return value


class ExporterConfig(Settings):
    traces: OTLPConfig | None = None
    metrics: OTLPConfig | None = None
    logs: OTLPConfig | None = None
    resource_attributes: dict[str, str] = Field(default_factory=dict)


class RuntimeTelemetry(Settings):
    runtime: Literal["", "adk-go", "claude", "codex"] = ""
    agent_name: str = ""
    agent_namespace: str = ""
    provider: str = ""
    model: str = ""
    capture_content: bool = False
    capture_raw_api_bodies: bool = False
    openai_native_tracing: bool = False
    max_capture_bytes: int = Field(default=0, ge=0, le=65536)
    exporters: ExporterConfig | None = None

    @model_validator(mode="after")
    def complete_identity(self):
        if self.exporters is not None:
            for key in ("service.name", "service.namespace"):
                if key in self.exporters.resource_attributes:
                    raise ValueError(f"runtime telemetry resource_attributes must not override compiled {key}")
        present = sum(bool(value) for value in (self.runtime, self.agent_name, self.agent_namespace))
        if present not in (0, 3):
            raise ValueError("telemetry identity requires a runtime, agent name, and namespace together")
        if any(value.strip() != value for value in (self.agent_name, self.agent_namespace, self.provider, self.model)):
            raise ValueError("telemetry identity must not have surrounding whitespace")
        return self


class TokenPropagationConfig(Settings):
    enabled: bool = False
    sts_well_known_uri: str = ""
    resources: list[str] = Field(default_factory=list)
    audiences: list[str] = Field(default_factory=list)

    @field_validator("sts_well_known_uri")
    @classmethod
    def optional_http_endpoint(cls, value: str) -> str:
        return OTLPConfig.http_endpoint(value) if value else value

    @field_validator("resources", "audiences")
    @classmethod
    def unpadded_values(cls, values: list[str]) -> list[str]:
        if any(not value or value.strip() != value for value in values):
            raise ValueError("values must be nonempty and unpadded")
        return values


class RuntimeConfig(Settings):
    control_plane_url: str = ""
    gateway_url: str = ""
    name: str = ""
    namespace: str = ""
    log_level: Literal["", "debug", "info", "warn", "error"] = ""
    host: str = ""
    port: str = ""
    max_content_length: int | None = Field(default=None, ge=0)
    token_propagation: TokenPropagationConfig = Field(default_factory=TokenPropagationConfig)
    file_search_tools: bool = False
    bash_venv_path: str = ""
    telemetry: RuntimeTelemetry = Field(default_factory=RuntimeTelemetry)

    @field_validator("control_plane_url", "gateway_url")
    @classmethod
    def optional_http_endpoint(cls, value: str) -> str:
        return OTLPConfig.http_endpoint(value) if value else value

    @field_validator("port")
    @classmethod
    def valid_port(cls, value: str) -> str:
        if value and (not value.isdecimal() or not 1 <= int(value) <= 65535):
            raise ValueError("must be between 1 and 65535")
        return value
