import logging
from typing import Any, Callable, Literal, Optional, Union

import httpx
from agentsts.adk import ADKTokenPropagationPlugin
from google.adk.agents import Agent
from google.adk.agents.base_agent import BaseAgent
from google.adk.agents.llm_agent import ToolUnion
from google.adk.agents.remote_a2a_agent import AGENT_CARD_WELL_KNOWN_PATH, DEFAULT_TIMEOUT, RemoteA2aAgent
from google.adk.code_executors.base_code_executor import BaseCodeExecutor
from google.adk.models.anthropic_llm import Claude as ClaudeLLM
from google.adk.models.google_llm import Gemini as GeminiLLM
from google.adk.models.lite_llm import LiteLlm
from google.adk.tools.agent_tool import AgentTool
from google.adk.tools.mcp_tool import McpToolset, SseConnectionParams, StreamableHTTPConnectionParams
from google.adk.tools.mcp_tool import mcp_session_manager
from mcp.client.streamable_http import streamablehttp_client
from pydantic import BaseModel, Field

from kagent.adk.sandbox_code_executer import SandboxedLocalCodeExecutor
from kagent.adk.models._ssl import create_ssl_context, load_client_certificate

from .models import AzureOpenAI as OpenAIAzure
from .models import OpenAI as OpenAINative

logger = logging.getLogger(__name__)

# Proxy host header used for Gateway API routing when using a proxy
PROXY_HOST_HEADER = "x-kagent-host"

# Flag to track if we've monkey-patched MCPSessionManager
_MCP_SESSION_MANAGER_PATCHED = False


def _patch_mcp_session_manager_for_tls():
    """Monkey patch MCPSessionManager._create_client to use httpx_client_factory if available.

    This allows us to inject TLS configuration (including client certificates) into
    the httpx client used by StreamableHTTPConnectionParams.
    """
    global _MCP_SESSION_MANAGER_PATCHED
    if _MCP_SESSION_MANAGER_PATCHED:
        return

    original_create_client = mcp_session_manager.MCPSessionManager._create_client

    def patched_create_client(self, merged_headers=None):
        """Patched version that checks for httpx_client_factory in StreamableHTTPConnectionParams."""
        from datetime import timedelta

        if merged_headers is None:
            merged_headers = {}

        if isinstance(self._connection_params, StreamableHTTPConnectionParams):
            # Check if httpx_client_factory is set (for TLS configuration)
            httpx_client_factory = getattr(self._connection_params, "httpx_client_factory", None)
            if httpx_client_factory is not None:
                # Use the custom factory
                client = streamablehttp_client(
                    url=self._connection_params.url,
                    headers=merged_headers,
                    timeout=timedelta(seconds=self._connection_params.timeout),
                    sse_read_timeout=timedelta(seconds=self._connection_params.sse_read_timeout),
                    terminate_on_close=self._connection_params.terminate_on_close,
                    httpx_client_factory=httpx_client_factory,
                )
                return client

        # Fall back to original implementation
        return original_create_client(self, merged_headers)

    # Replace the method
    mcp_session_manager.MCPSessionManager._create_client = patched_create_client
    _MCP_SESSION_MANAGER_PATCHED = True


def _apply_mcp_tls_config(
        connection_params: Union[StreamableHTTPConnectionParams, SseConnectionParams]
) -> Union[StreamableHTTPConnectionParams, SseConnectionParams]:
    """Apply TLS configuration (including client certificates) to connection params.

    Since Google ADK's StreamableHTTPConnectionParams and SseConnectionParams don't
    natively support TLSClientCertPath, we use the httpx_client_factory mechanism
    for StreamableHTTPConnectionParams to inject TLS configuration.

    For SseConnectionParams, TLS configuration is not supported yet (no factory mechanism).

    Args:
        connection_params: The connection parameters containing TLS configuration

    Returns:
        Modified connection_params with TLS configuration applied (if applicable)
    """
    # Check if TLS client certificate path is configured
    tls_client_cert_path = getattr(connection_params, "tls_client_cert_path", None)
    tls_disable_verify = getattr(connection_params, "insecure_tls_verify", None)

    # If no TLS configuration, return early
    if not tls_client_cert_path and not tls_disable_verify:
        return connection_params

    # Only StreamableHTTPConnectionParams supports httpx_client_factory
    if not isinstance(connection_params, StreamableHTTPConnectionParams):
        return connection_params

    # Load client certificate if path is provided
    client_cert = None
    ca_cert_path = None
    if tls_client_cert_path:
        try:
            cert_file, key_file, ca_cert_path = load_client_certificate(tls_client_cert_path)
            client_cert = (cert_file, key_file)
        except Exception as e:
            logger.error("Failed to load client certificate from %s: %s", tls_client_cert_path, e, exc_info=True)
            raise

    # Create SSL context
    ssl_context = create_ssl_context(
        disable_verify=bool(tls_disable_verify),
        ca_cert_path=ca_cert_path,  # Use CA cert from client cert directory if available
        disable_system_cas=False,
    )

    # Create a custom httpx_client_factory that includes TLS configuration
    # Note: StreamableHTTPConnectionParams doesn't have httpx_client_factory as a native attribute,
    # so we use object.__setattr__ to set it
    # The closure will automatically capture ssl_context and client_cert from the outer scope
    def create_tls_httpx_client(
            headers: dict[str, str] | None = None,
            timeout: httpx.Timeout | None = None,
            auth: httpx.Auth | None = None,
    ) -> httpx.AsyncClient:
        """Create httpx client with TLS configuration including client certificates.

        This function creates a new httpx.AsyncClient with TLS configuration
        (SSL context and client certificate) while preserving MCP defaults.

        NOTE: This function is called at RUNTIME when MCP requests are made, not at startup.
        The closure automatically captures ssl_context and client_cert from the outer scope.
        """
        # Create a new client with TLS configuration
        # This preserves MCP defaults (follow_redirects=True, etc.) while adding TLS
        client_kwargs = {
            "follow_redirects": True,  # MCP default
            "verify": ssl_context,
            "timeout": timeout or httpx.Timeout(30.0),  # MCP default timeout
        }
        if client_cert:
            client_kwargs["cert"] = client_cert
        if headers:
            client_kwargs["headers"] = headers
        if auth:
            client_kwargs["auth"] = auth

        return httpx.AsyncClient(**client_kwargs)

    # Replace the httpx_client_factory with our TLS-enabled version
    # Use object.__setattr__ to bypass Pydantic's validation and set the attribute
    object.__setattr__(connection_params, "httpx_client_factory", create_tls_httpx_client)

    # Ensure MCPSessionManager is patched to use httpx_client_factory
    _patch_mcp_session_manager_for_tls()

    return connection_params


def _extract_and_set_tls_fields(obj: dict, instance: BaseModel) -> None:
    """Extract TLS fields from JSON and set them on params object.
    
    Helper function to avoid code duplication between HttpMcpServerConfig and SseMcpServerConfig.
    """
    params = obj.get("params") if isinstance(obj, dict) else None
    if isinstance(params, dict):
        tls_client_cert_path = params.get("tls_client_cert_path")
        insecure_tls_verify = params.get("insecure_tls_verify")
        if tls_client_cert_path is not None or insecure_tls_verify is not None:
            instance.params.tls_client_cert_path = tls_client_cert_path
            instance.params.insecure_tls_verify = insecure_tls_verify


class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)

    @classmethod
    def model_validate(cls, obj, *, strict=None, from_attributes=None, context=None):
        """Custom validation to preserve TLS fields from JSON that Google ADK ignores."""
        instance = super().model_validate(obj, strict=strict, from_attributes=from_attributes, context=context)
        if isinstance(obj, dict):
            _extract_and_set_tls_fields(obj, instance)
        return instance


class SseMcpServerConfig(BaseModel):
    params: SseConnectionParams
    tools: list[str] = Field(default_factory=list)

    @classmethod
    def model_validate(cls, obj, *, strict=None, from_attributes=None, context=None):
        """Custom validation to preserve TLS fields from JSON that Google ADK ignores."""
        instance = super().model_validate(obj, strict=strict, from_attributes=from_attributes, context=context)
        if isinstance(obj, dict):
            _extract_and_set_tls_fields(obj, instance)
        return instance


class RemoteAgentConfig(BaseModel):
    name: str
    url: str
    headers: dict[str, Any] | None = None
    timeout: float = DEFAULT_TIMEOUT
    description: str = ""


class BaseLLM(BaseModel):
    model: str
    headers: dict[str, str] | None = None

    # TLS/SSL configuration (applies to all model types)
    tls_disable_verify: bool | None = None
    tls_ca_cert_path: str | None = None
    tls_disable_system_cas: bool | None = None


class OpenAI(BaseLLM):
    base_url: str | None = None
    frequency_penalty: float | None = None
    max_tokens: int | None = None
    n: int | None = None
    presence_penalty: float | None = None
    reasoning_effort: str | None = None
    seed: int | None = None
    temperature: float | None = None
    timeout: int | None = None
    top_p: float | None = None

    type: Literal["openai"]


class AzureOpenAI(BaseLLM):
    type: Literal["azure_openai"]


class Anthropic(BaseLLM):
    base_url: str | None = None

    type: Literal["anthropic"]


class GeminiVertexAI(BaseLLM):
    type: Literal["gemini_vertex_ai"]


class GeminiAnthropic(BaseLLM):
    type: Literal["gemini_anthropic"]


class Ollama(BaseLLM):
    type: Literal["ollama"]


class Gemini(BaseLLM):
    type: Literal["gemini"]


class Bedrock(BaseLLM):
    region: str | None = None
    type: Literal["bedrock"]


class AgentConfig(BaseModel):
    model: Union[OpenAI, Anthropic, GeminiVertexAI, GeminiAnthropic, Ollama, AzureOpenAI, Gemini, Bedrock] = Field(
        discriminator="type"
    )
    description: str
    instruction: str
    http_tools: list[HttpMcpServerConfig] | None = None  # Streamable HTTP MCP tools
    sse_tools: list[SseMcpServerConfig] | None = None  # SSE MCP tools
    remote_agents: list[RemoteAgentConfig] | None = None  # remote agents
    execute_code: bool | None = None
    # This stream option refers to LLM response streaming, not A2A streaming
    stream: bool | None = None

    @classmethod
    def model_validate(cls, obj, *, strict=None, from_attributes=None, context=None):
        """Custom validation to extract TLS fields from http_tools and set them on params objects.

        This is needed because Pydantic v2 doesn't call model_validate on nested models,
        so HttpMcpServerConfig.model_validate is never called. We extract TLS fields from
        the original JSON and set them on the params objects after validation.
        """
        # Call parent validation to create the instance
        instance = super().model_validate(obj, strict=strict, from_attributes=from_attributes, context=context)

        # After validation, manually set TLS fields on params objects from the original JSON
        if isinstance(obj, dict) and "http_tools" in obj and instance.http_tools:
            http_tools_data = obj.get("http_tools", [])
            for http_tool_data, http_tool_instance in zip(http_tools_data, instance.http_tools):
                params_data = http_tool_data.get("params") if isinstance(http_tool_data, dict) else None
                if isinstance(params_data, dict):
                    tls_client_cert_path = params_data.get("tls_client_cert_path")
                    insecure_tls_verify = params_data.get("insecure_tls_verify")
                    if tls_client_cert_path is not None or insecure_tls_verify is not None:
                        # Set TLS fields on the params object using object.__setattr__ to bypass Pydantic validation
                        # These fields are not in the Pydantic model, so we need to set them as regular attributes
                        object.__setattr__(http_tool_instance.params, "tls_client_cert_path", tls_client_cert_path)
                        object.__setattr__(http_tool_instance.params, "insecure_tls_verify", insecure_tls_verify)

        return instance

    def to_agent(self, name: str, sts_integration: Optional[ADKTokenPropagationPlugin] = None) -> Agent:
        if name is None or not str(name).strip():
            raise ValueError("Agent name must be a non-empty string.")
        tools: list[ToolUnion] = []
        header_provider = None
        if sts_integration:
            header_provider = sts_integration.header_provider
        if self.http_tools:
            for http_tool in self.http_tools:  # add http tools
                # Apply TLS configuration before creating McpToolset
                # This modifies the connection_params to include TLS settings via httpx_client_factory
                connection_params = _apply_mcp_tls_config(http_tool.params)
                mcp_toolset = McpToolset(
                    connection_params=connection_params, tool_filter=http_tool.tools, header_provider=header_provider
                )
                tools.append(mcp_toolset)
        if self.sse_tools:
            for sse_tool in self.sse_tools:  # add sse tools
                # If the proxy is configured, the url and headers are set in the json configuration
                tools.append(
                    McpToolset(
                        connection_params=sse_tool.params, tool_filter=sse_tool.tools, header_provider=header_provider
                    )
                )
        if self.remote_agents:
            for remote_agent in self.remote_agents:  # Add remote agents as tools
                # Prepare httpx client parameters
                timeout = httpx.Timeout(timeout=remote_agent.timeout)
                headers: dict[str, str] | None = remote_agent.headers
                base_url: str | None = None
                event_hooks: dict[str, list[Callable[[httpx.Request], None]]] | None = None

                # If headers includes the proxy host header, it means we're using a proxy
                # RemoteA2aAgent may use URLs from agent card response, so we need to
                # rewrite all request URLs to use the proxy URL while preserving the proxy host header
                if remote_agent.headers and PROXY_HOST_HEADER in remote_agent.headers:
                    # Parse the proxy URL to extract base URL
                    from urllib.parse import urlparse as parse_url

                    parsed_proxy = parse_url(remote_agent.url)
                    proxy_base = f"{parsed_proxy.scheme}://{parsed_proxy.netloc}"
                    target_host = remote_agent.headers[PROXY_HOST_HEADER]

                    # Event hook to rewrite request URLs to use proxy while preserving the proxy host header
                    # Note: Relative paths are handled by base_url below, so they'll already point to proxy_base
                    def make_rewrite_url_to_proxy(proxy_base: str, target_host: str) -> Callable[[httpx.Request], None]:
                        async def rewrite_url_to_proxy(request: httpx.Request) -> None:
                            parsed = parse_url(str(request.url))
                            proxy_netloc = parse_url(proxy_base).netloc

                            # If URL is absolute and points to a different host, rewrite to the proxy base URL
                            if parsed.netloc and parsed.netloc != proxy_netloc:
                                # This is an absolute URL pointing to the target service, rewrite it
                                new_url = f"{proxy_base}{parsed.path}"
                                if parsed.query:
                                    new_url += f"?{parsed.query}"
                                request.url = httpx.URL(new_url)

                            # Always set proxy host header for Gateway API routing
                            request.headers[PROXY_HOST_HEADER] = target_host

                        return rewrite_url_to_proxy

                    # Set base_url so relative paths work correctly with httpx
                    # httpx requires either base_url or absolute URLs - relative paths will fail without base_url
                    base_url = proxy_base
                    event_hooks = {"request": [make_rewrite_url_to_proxy(proxy_base, target_host)]}

                # Build client kwargs (httpx doesn't accept None for base_url/event_hooks)
                client_kwargs = {"timeout": timeout}
                if headers:
                    client_kwargs["headers"] = headers
                if base_url:
                    client_kwargs["base_url"] = base_url
                if event_hooks:
                    client_kwargs["event_hooks"] = event_hooks
                client = httpx.AsyncClient(**client_kwargs)

                remote_a2a_agent = RemoteA2aAgent(
                    name=remote_agent.name,
                    agent_card=f"{remote_agent.url}{AGENT_CARD_WELL_KNOWN_PATH}",
                    description=remote_agent.description,
                    httpx_client=client,
                )

                tools.append(AgentTool(agent=remote_a2a_agent))

        extra_headers = self.model.headers or {}

        code_executor = SandboxedLocalCodeExecutor() if self.execute_code else None

        if self.model.type == "openai":
            model = OpenAINative(
                type="openai",
                base_url=self.model.base_url,
                default_headers=extra_headers,
                frequency_penalty=self.model.frequency_penalty,
                max_tokens=self.model.max_tokens,
                model=self.model.model,
                n=self.model.n,
                presence_penalty=self.model.presence_penalty,
                reasoning_effort=self.model.reasoning_effort,
                seed=self.model.seed,
                temperature=self.model.temperature,
                timeout=self.model.timeout,
                top_p=self.model.top_p,
                # TLS configuration
                tls_disable_verify=self.model.tls_disable_verify,
                tls_ca_cert_path=self.model.tls_ca_cert_path,
                tls_disable_system_cas=self.model.tls_disable_system_cas,
            )
        elif self.model.type == "anthropic":
            model = LiteLlm(
                model=f"anthropic/{self.model.model}", base_url=self.model.base_url, extra_headers=extra_headers
            )
        elif self.model.type == "gemini_vertex_ai":
            model = GeminiLLM(model=self.model.model)
        elif self.model.type == "gemini_anthropic":
            model = ClaudeLLM(model=self.model.model)
        elif self.model.type == "ollama":
            model = LiteLlm(model=f"ollama_chat/{self.model.model}", extra_headers=extra_headers)
        elif self.model.type == "azure_openai":
            model = OpenAIAzure(
                model=self.model.model,
                type="azure_openai",
                default_headers=extra_headers,
                # TLS configuration
                tls_disable_verify=self.model.tls_disable_verify,
                tls_ca_cert_path=self.model.tls_ca_cert_path,
                tls_disable_system_cas=self.model.tls_disable_system_cas,
            )
        elif self.model.type == "gemini":
            model = self.model.model
        elif self.model.type == "bedrock":
            # LiteLLM handles Bedrock via boto3 internally when model starts with "bedrock/"
            model = LiteLlm(model=f"bedrock/{self.model.model}", extra_headers=extra_headers)
        else:
            raise ValueError(f"Invalid model type: {self.model.type}")
        return Agent(
            name=name,
            model=model,
            description=self.description,
            instruction=self.instruction,
            tools=tools,
            code_executor=code_executor,
        )
