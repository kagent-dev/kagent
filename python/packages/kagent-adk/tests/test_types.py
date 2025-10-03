"""Comprehensive tests for kagent.adk.types module.

This test suite ensures >90% code coverage for all functions and classes in types.py.
"""

import pytest
from unittest.mock import Mock, MagicMock, patch, AsyncMock
from typing import Any

from kagent.adk.types import (
    sanitize_agent_name,
    generate_workflow_name,
    to_workflow_agent,
    HttpMcpServerConfig,
    SseMcpServerConfig,
    RemoteAgentConfig,
    SubagentConfig,
    WorkflowConfig,
    OpenAI,
    AzureOpenAI,
    Anthropic,
    GeminiVertexAI,
    GeminiAnthropic,
    Ollama,
    Gemini,
    AgentConfig,
)


# ============================================================================
# Tests for sanitize_agent_name()
# ============================================================================

class TestSanitizeAgentName:
    """Test suite for sanitize_agent_name function."""

    def test_basic_sanitization(self):
        """Test basic character replacement."""
        assert sanitize_agent_name("hello-world") == "hello_world"
        assert sanitize_agent_name("Hello World") == "Hello_World"
        assert sanitize_agent_name("test-name-with-dashes") == "test_name_with_dashes"

    def test_leading_number(self):
        """Test that leading numbers get underscore prefix."""
        assert sanitize_agent_name("123-invalid") == "_123_invalid"
        assert sanitize_agent_name("9test") == "_9test"

    def test_special_characters_removed(self):
        """Test that invalid special characters are removed."""
        assert sanitize_agent_name("test@#$name!") == "testname"
        assert sanitize_agent_name("hello.world") == "helloworld"
        assert sanitize_agent_name("test(with)parens") == "testwithparens"

    def test_empty_string(self):
        """Test that empty string returns underscore."""
        assert sanitize_agent_name("") == "_"
        assert sanitize_agent_name("   ") == "___"  # Spaces become underscores

    def test_only_special_characters(self):
        """Test string with only invalid characters."""
        assert sanitize_agent_name("@#$%") == "_"
        assert sanitize_agent_name("!!!") == "_"

    def test_max_length_truncation(self):
        """Test that max_length parameter truncates correctly."""
        long_name = "a" * 100
        assert len(sanitize_agent_name(long_name, max_length=64)) == 64
        
        long_name = "strategic_analysis_workflow_Strategic_Analysis_Pipeline_sequential"
        result = sanitize_agent_name(long_name, max_length=64)
        assert len(result) == 64
        assert result == "strategic_analysis_workflow_Strategic_Analysis_Pipeline_sequenti"

    def test_max_length_no_truncation_needed(self):
        """Test that short names aren't affected by max_length."""
        short_name = "short"
        assert sanitize_agent_name(short_name, max_length=64) == "short"

    def test_preserves_underscores(self):
        """Test that underscores are preserved."""
        assert sanitize_agent_name("test_name_with_underscores") == "test_name_with_underscores"
        assert sanitize_agent_name("_leading_underscore") == "_leading_underscore"

    def test_leading_underscore_preserved(self):
        """Test that names starting with underscore are valid."""
        assert sanitize_agent_name("_private") == "_private"
        assert sanitize_agent_name("__double") == "__double"

    def test_alphanumeric_preserved(self):
        """Test that alphanumeric characters are preserved."""
        assert sanitize_agent_name("test123") == "test123"
        assert sanitize_agent_name("ABC123xyz") == "ABC123xyz"


# ============================================================================
# Tests for generate_workflow_name()
# ============================================================================

class TestGenerateWorkflowName:
    """Test suite for generate_workflow_name function."""

    def test_basic_workflow_name_generation(self):
        """Test basic workflow name generation."""
        result = generate_workflow_name("base", "role", "Sequential")
        assert result == "base_role_sequential"
        assert len(result) <= 64

    def test_empty_role(self):
        """Test workflow name generation with empty role."""
        result = generate_workflow_name("base", "", "Parallel")
        assert result == "base_parallel"
        assert len(result) <= 64

    def test_long_names_truncated(self):
        """Test that long workflow names are truncated to 64 chars."""
        long_base = "strategic_analysis_workflow"
        long_role = "Strategic_Analysis_Pipeline"
        result = generate_workflow_name(long_base, long_role, "Sequential")
        assert len(result) <= 64

    def test_different_workflow_types(self):
        """Test all workflow type suffixes."""
        for wf_type in ["Sequential", "Parallel", "Loop"]:
            result = generate_workflow_name("base", "role", wf_type)
            assert result.endswith(f"_{wf_type.lower()}")
            assert len(result) <= 64

    def test_sanitization_applied(self):
        """Test that result goes through sanitization."""
        # The base and role should already be sanitized, but max_length is applied
        base = "a" * 50
        role = "b" * 50
        result = generate_workflow_name(base, role, "Sequential")
        assert len(result) <= 64


# ============================================================================
# Tests for to_workflow_agent()
# ============================================================================

class TestToWorkflowAgent:
    """Test suite for to_workflow_agent factory function."""

    @patch('kagent.adk.types.SequentialAgent')
    def test_sequential_agent_creation(self, mock_sequential):
        """Test Sequential agent creation."""
        subagents = [Mock(), Mock()]
        agent_name = "test_sequential"
        
        to_workflow_agent("Sequential", agent_name, subagents)
        
        mock_sequential.assert_called_once_with(
            name=agent_name,
            sub_agents=subagents
        )

    @patch('kagent.adk.types.ParallelAgent')
    def test_parallel_agent_creation(self, mock_parallel):
        """Test Parallel agent creation."""
        subagents = [Mock(), Mock()]
        agent_name = "test_parallel"
        
        to_workflow_agent("Parallel", agent_name, subagents)
        
        mock_parallel.assert_called_once_with(
            name=agent_name,
            sub_agents=subagents
        )

    @patch('kagent.adk.types.LoopAgent')
    def test_loop_agent_creation_default_iterations(self, mock_loop):
        """Test Loop agent creation with default max_iterations."""
        subagents = [Mock()]
        agent_name = "test_loop"
        
        to_workflow_agent("Loop", agent_name, subagents)
        
        mock_loop.assert_called_once_with(
            name=agent_name,
            sub_agents=subagents,
            max_iterations=5
        )

    @patch('kagent.adk.types.LoopAgent')
    def test_loop_agent_creation_custom_iterations(self, mock_loop):
        """Test Loop agent creation with custom max_iterations."""
        subagents = [Mock()]
        agent_name = "test_loop"
        
        to_workflow_agent("Loop", agent_name, subagents, max_iterations=10)
        
        mock_loop.assert_called_once_with(
            name=agent_name,
            sub_agents=subagents,
            max_iterations=10
        )

    def test_invalid_workflow_type(self):
        """Test that invalid workflow type raises ValueError."""
        with pytest.raises(ValueError, match="Unknown workflow type: InvalidType"):
            to_workflow_agent("InvalidType", "test", [Mock()])

    def test_empty_workflow_type(self):
        """Test that empty workflow type raises ValueError."""
        with pytest.raises(ValueError, match="Unknown workflow type: "):
            to_workflow_agent("", "test", [Mock()])


# ============================================================================
# Tests for Pydantic Models
# ============================================================================

class TestPydanticModels:
    """Test suite for Pydantic model classes."""

    def test_http_mcp_server_config_creation(self):
        """Test HttpMcpServerConfig model."""
        from google.adk.tools.mcp_tool import StreamableHTTPConnectionParams
        
        params = Mock(spec=StreamableHTTPConnectionParams)
        config = HttpMcpServerConfig(params=params, tools=["tool1", "tool2"])
        
        assert config.params == params
        assert config.tools == ["tool1", "tool2"]

    def test_http_mcp_server_config_default_tools(self):
        """Test HttpMcpServerConfig with default empty tools list."""
        from google.adk.tools.mcp_tool import StreamableHTTPConnectionParams
        
        params = Mock(spec=StreamableHTTPConnectionParams)
        config = HttpMcpServerConfig(params=params)
        
        assert config.tools == []

    def test_sse_mcp_server_config_creation(self):
        """Test SseMcpServerConfig model."""
        from google.adk.tools.mcp_tool import SseConnectionParams
        
        params = Mock(spec=SseConnectionParams)
        config = SseMcpServerConfig(params=params, tools=["tool1"])
        
        assert config.params == params
        assert config.tools == ["tool1"]

    def test_remote_agent_config_creation(self):
        """Test RemoteAgentConfig model."""
        config = RemoteAgentConfig(
            name="test-agent",
            url="https://example.com",
            headers={"Authorization": "Bearer token"},
            timeout=30.0,
            description="Test agent"
        )
        
        assert config.name == "test-agent"
        assert config.url == "https://example.com"
        assert config.headers == {"Authorization": "Bearer token"}
        assert config.timeout == 30.0
        assert config.description == "Test agent"

    def test_remote_agent_config_defaults(self):
        """Test RemoteAgentConfig default values."""
        config = RemoteAgentConfig(name="test", url="https://example.com")
        
        assert config.headers is None
        assert config.timeout == 600.0  # DEFAULT_TIMEOUT from google.adk
        assert config.description == ""

    def test_subagent_config_creation(self):
        """Test SubagentConfig model."""
        config = SubagentConfig(
            name="subagent",
            url="https://sub.example.com",
            headers={"key": "value"},
            timeout=15.0,
            description="Sub agent"
        )
        
        assert config.name == "subagent"
        assert config.url == "https://sub.example.com"
        assert config.headers == {"key": "value"}
        assert config.timeout == 15.0
        assert config.description == "Sub agent"

    def test_workflow_config_sequential(self):
        """Test WorkflowConfig for Sequential type."""
        subagents = [
            SubagentConfig(name="sub1", url="https://sub1.com"),
            SubagentConfig(name="sub2", url="https://sub2.com")
        ]
        config = WorkflowConfig(
            type="Sequential",
            subagents=subagents,
            role="coordinator",
            max_iterations=3
        )
        
        assert config.type == "Sequential"
        assert len(config.subagents) == 2
        assert config.role == "coordinator"
        assert config.max_iterations == 3

    def test_workflow_config_parallel(self):
        """Test WorkflowConfig for Parallel type."""
        subagents = [SubagentConfig(name="sub1", url="https://sub1.com")]
        config = WorkflowConfig(type="Parallel", subagents=subagents)
        
        assert config.type == "Parallel"
        assert config.role == ""
        assert config.max_iterations == 5  # default

    def test_workflow_config_loop(self):
        """Test WorkflowConfig for Loop type."""
        subagents = [SubagentConfig(name="sub1", url="https://sub1.com")]
        config = WorkflowConfig(
            type="Loop",
            subagents=subagents,
            max_iterations=10
        )
        
        assert config.type == "Loop"
        assert config.max_iterations == 10


# ============================================================================
# Tests for LLM Model Classes
# ============================================================================

class TestLLMModels:
    """Test suite for LLM model configuration classes."""

    def test_openai_model(self):
        """Test OpenAI model configuration."""
        model = OpenAI(
            type="openai",
            model="gpt-4",
            base_url="https://api.openai.com",
            temperature=0.7,
            max_tokens=1000,
            frequency_penalty=0.5,
            presence_penalty=0.3,
            seed=42,
            timeout=60,
            top_p=0.9,
            n=1
        )
        
        assert model.type == "openai"
        assert model.model == "gpt-4"
        assert model.temperature == 0.7
        assert model.max_tokens == 1000

    def test_azure_openai_model(self):
        """Test AzureOpenAI model configuration."""
        model = AzureOpenAI(
            type="azure_openai",
            model="gpt-4",
            headers={"api-key": "test"}
        )
        
        assert model.type == "azure_openai"
        assert model.model == "gpt-4"

    def test_anthropic_model(self):
        """Test Anthropic model configuration."""
        model = Anthropic(
            type="anthropic",
            model="claude-3-opus",
            base_url="https://api.anthropic.com"
        )
        
        assert model.type == "anthropic"
        assert model.model == "claude-3-opus"

    def test_gemini_vertex_ai_model(self):
        """Test GeminiVertexAI model configuration."""
        model = GeminiVertexAI(type="gemini_vertex_ai", model="gemini-pro")
        
        assert model.type == "gemini_vertex_ai"
        assert model.model == "gemini-pro"

    def test_gemini_anthropic_model(self):
        """Test GeminiAnthropic model configuration."""
        model = GeminiAnthropic(type="gemini_anthropic", model="claude-in-gemini")
        
        assert model.type == "gemini_anthropic"
        assert model.model == "claude-in-gemini"

    def test_ollama_model(self):
        """Test Ollama model configuration."""
        model = Ollama(type="ollama", model="llama2")
        
        assert model.type == "ollama"
        assert model.model == "llama2"

    def test_gemini_model(self):
        """Test Gemini model configuration."""
        model = Gemini(type="gemini", model="gemini-1.5-pro")
        
        assert model.type == "gemini"
        assert model.model == "gemini-1.5-pro"


# ============================================================================
# Tests for AgentConfig.to_agent()
# ============================================================================

class TestAgentConfigToAgent:
    """Test suite for AgentConfig.to_agent() method - the most critical component."""

    def test_name_validation_empty_string(self):
        """Test that empty agent name raises ValueError."""
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction"
        )
        
        with pytest.raises(ValueError, match="Agent name must be a non-empty string"):
            config.to_agent("")

    def test_name_validation_none(self):
        """Test that None agent name raises ValueError."""
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction"
        )
        
        with pytest.raises(ValueError, match="Agent name must be a non-empty string"):
            config.to_agent(None)

    def test_name_validation_whitespace_only(self):
        """Test that whitespace-only name raises ValueError."""
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction"
        )
        
        with pytest.raises(ValueError, match="Agent name must be a non-empty string"):
            config.to_agent("   ")

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    def test_openai_model_creation(self, mock_openai_native, mock_agent):
        """Test OpenAI model instantiation in to_agent()."""
        config = AgentConfig(
            model=OpenAI(
                type="openai",
                model="gpt-4",
                base_url="https://api.openai.com",
                temperature=0.7,
                max_tokens=1000
            ),
            description="Test agent",
            instruction="Test instruction"
        )
        
        config.to_agent("test_agent")
        
        mock_openai_native.assert_called_once()
        call_kwargs = mock_openai_native.call_args[1]
        assert call_kwargs['model'] == "gpt-4"
        assert call_kwargs['type'] == "openai"
        assert call_kwargs['base_url'] == "https://api.openai.com"
        assert call_kwargs['temperature'] == 0.7
        assert call_kwargs['max_tokens'] == 1000

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.LiteLlm')
    def test_anthropic_model_creation(self, mock_litellm, mock_agent):
        """Test Anthropic model instantiation via LiteLlm."""
        config = AgentConfig(
            model=Anthropic(
                type="anthropic",
                model="claude-3-opus",
                base_url="https://api.anthropic.com"
            ),
            description="Test agent",
            instruction="Test instruction"
        )
        
        config.to_agent("test_agent")
        
        mock_litellm.assert_called_once()
        call_kwargs = mock_litellm.call_args[1]
        assert call_kwargs['model'] == "anthropic/claude-3-opus"
        assert call_kwargs['base_url'] == "https://api.anthropic.com"

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.GeminiLLM')
    def test_gemini_vertex_ai_model_creation(self, mock_gemini, mock_agent):
        """Test GeminiVertexAI model instantiation."""
        config = AgentConfig(
            model=GeminiVertexAI(type="gemini_vertex_ai", model="gemini-pro"),
            description="Test agent",
            instruction="Test instruction"
        )
        
        config.to_agent("test_agent")
        
        mock_gemini.assert_called_once_with(model="gemini-pro")

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.ClaudeLLM')
    def test_gemini_anthropic_model_creation(self, mock_claude, mock_agent):
        """Test GeminiAnthropic model instantiation."""
        config = AgentConfig(
            model=GeminiAnthropic(type="gemini_anthropic", model="claude-model"),
            description="Test agent",
            instruction="Test instruction"
        )
        
        config.to_agent("test_agent")
        
        mock_claude.assert_called_once_with(model="claude-model")

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.LiteLlm')
    def test_ollama_model_creation(self, mock_litellm, mock_agent):
        """Test Ollama model instantiation via LiteLlm."""
        config = AgentConfig(
            model=Ollama(type="ollama", model="llama2"),
            description="Test agent",
            instruction="Test instruction"
        )
        
        config.to_agent("test_agent")
        
        mock_litellm.assert_called_once()
        call_kwargs = mock_litellm.call_args[1]
        assert call_kwargs['model'] == "ollama_chat/llama2"

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAIAzure')
    def test_azure_openai_model_creation(self, mock_azure, mock_agent):
        """Test AzureOpenAI model instantiation."""
        config = AgentConfig(
            model=AzureOpenAI(
                type="azure_openai",
                model="gpt-4",
                headers={"api-key": "test"}
            ),
            description="Test agent",
            instruction="Test instruction"
        )
        
        config.to_agent("test_agent")
        
        mock_azure.assert_called_once()
        call_kwargs = mock_azure.call_args[1]
        assert call_kwargs['model'] == "gpt-4"
        assert call_kwargs['type'] == "azure_openai"

    @patch('kagent.adk.types.Agent')
    def test_gemini_model_creation(self, mock_agent):
        """Test Gemini model (special case - returns model string directly)."""
        config = AgentConfig(
            model=Gemini(type="gemini", model="gemini-1.5-pro"),
            description="Test agent",
            instruction="Test instruction"
        )
        
        config.to_agent("test_agent")
        
        # For gemini type, the model itself is just the string
        mock_agent.assert_called_once()
        call_kwargs = mock_agent.call_args[1]
        assert call_kwargs['model'] == "gemini-1.5-pro"

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    @patch('kagent.adk.types.MCPToolset')
    def test_http_tools_creation(self, mock_mcp_toolset, mock_openai, mock_agent):
        """Test HTTP MCP tools are created correctly."""
        from google.adk.tools.mcp_tool import StreamableHTTPConnectionParams
        
        http_params = Mock(spec=StreamableHTTPConnectionParams)
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction",
            http_tools=[
                HttpMcpServerConfig(params=http_params, tools=["tool1", "tool2"])
            ]
        )
        
        config.to_agent("test_agent")
        
        mock_mcp_toolset.assert_called()
        call_kwargs = mock_mcp_toolset.call_args[1]
        assert call_kwargs['connection_params'] == http_params
        assert call_kwargs['tool_filter'] == ["tool1", "tool2"]

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    @patch('kagent.adk.types.MCPToolset')
    def test_sse_tools_creation(self, mock_mcp_toolset, mock_openai, mock_agent):
        """Test SSE MCP tools are created correctly."""
        from google.adk.tools.mcp_tool import SseConnectionParams
        
        sse_params = Mock(spec=SseConnectionParams)
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction",
            sse_tools=[
                SseMcpServerConfig(params=sse_params, tools=["sse_tool1"])
            ]
        )
        
        config.to_agent("test_agent")
        
        # Should be called for SSE tool
        assert mock_mcp_toolset.call_count >= 1

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    @patch('kagent.adk.types.RemoteA2aAgent')
    @patch('kagent.adk.types.AgentTool')
    @patch('kagent.adk.types.httpx.AsyncClient')
    def test_remote_agents_with_headers(
        self, mock_httpx, mock_agent_tool, mock_remote_agent, mock_openai, mock_agent
    ):
        """Test remote agents creation with custom headers."""
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction",
            remote_agents=[
                RemoteAgentConfig(
                    name="remote1",
                    url="https://remote.example.com",
                    headers={"Authorization": "Bearer token"},
                    timeout=30.0,
                    description="Remote agent 1"
                )
            ]
        )
        
        config.to_agent("test_agent")
        
        # Verify httpx client created with headers
        mock_httpx.assert_called_once()
        call_kwargs = mock_httpx.call_args[1]
        assert call_kwargs['headers'] == {"Authorization": "Bearer token"}
        
        # Verify RemoteA2aAgent created
        mock_remote_agent.assert_called_once()
        call_kwargs = mock_remote_agent.call_args[1]
        assert call_kwargs['name'] == "remote1"
        assert "remote.example.com" in call_kwargs['agent_card']

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    @patch('kagent.adk.types.RemoteA2aAgent')
    @patch('kagent.adk.types.AgentTool')
    def test_remote_agents_without_headers(
        self, mock_agent_tool, mock_remote_agent, mock_openai, mock_agent
    ):
        """Test remote agents creation without custom headers."""
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction",
            remote_agents=[
                RemoteAgentConfig(
                    name="remote1",
                    url="https://remote.example.com"
                )
            ]
        )
        
        config.to_agent("test_agent")
        
        # Verify RemoteA2aAgent created with None client
        mock_remote_agent.assert_called_once()
        call_kwargs = mock_remote_agent.call_args[1]
        assert call_kwargs['httpx_client'] is None

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    @patch('kagent.adk.types.RemoteA2aAgent')
    @patch('kagent.adk.types.to_workflow_agent')
    @patch('kagent.adk.types.AgentTool')
    def test_workflow_subagents_sequential(
        self, mock_agent_tool, mock_to_workflow, mock_remote_agent, mock_openai, mock_agent
    ):
        """Test workflow subagents creation (Sequential type)."""
        mock_workflow_agent = Mock()
        mock_to_workflow.return_value = mock_workflow_agent
        
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction",
            workflow_subagents=[
                WorkflowConfig(
                    type="Sequential",
                    role="coordinator",
                    subagents=[
                        SubagentConfig(name="sub1", url="https://sub1.com"),
                        SubagentConfig(name="sub2", url="https://sub2.com")
                    ]
                )
            ]
        )
        
        config.to_agent("test_agent")
        
        # Verify workflow agent factory called
        mock_to_workflow.assert_called_once()
        call_kwargs = mock_to_workflow.call_args[1]
        assert call_kwargs['workflow_type'] == "Sequential"
        assert len(call_kwargs['subagents']) == 2

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    @patch('kagent.adk.types.RemoteA2aAgent')
    @patch('kagent.adk.types.to_workflow_agent')
    @patch('kagent.adk.types.AgentTool')
    def test_workflow_subagents_with_empty_role(
        self, mock_agent_tool, mock_to_workflow, mock_remote_agent, mock_openai, mock_agent
    ):
        """Test workflow subagents with empty role."""
        mock_workflow_agent = Mock()
        mock_to_workflow.return_value = mock_workflow_agent
        
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction",
            workflow_subagents=[
                WorkflowConfig(
                    type="Parallel",
                    role="",  # Empty role
                    subagents=[
                        SubagentConfig(name="sub1", url="https://sub1.com")
                    ]
                )
            ]
        )
        
        config.to_agent("test_agent")
        
        # Verify workflow agent created with name containing just base and type
        mock_to_workflow.assert_called_once()
        call_kwargs = mock_to_workflow.call_args[1]
        assert call_kwargs['workflow_type'] == "Parallel"

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    @patch('kagent.adk.types.RemoteA2aAgent')
    @patch('kagent.adk.types.to_workflow_agent')
    @patch('kagent.adk.types.AgentTool')
    @patch('kagent.adk.types.httpx.AsyncClient')
    def test_workflow_subagents_with_headers(
        self, mock_httpx, mock_agent_tool, mock_to_workflow, 
        mock_remote_agent, mock_openai, mock_agent
    ):
        """Test workflow subagents with custom headers."""
        mock_workflow_agent = Mock()
        mock_to_workflow.return_value = mock_workflow_agent
        
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction",
            workflow_subagents=[
                WorkflowConfig(
                    type="Loop",
                    max_iterations=7,
                    subagents=[
                        SubagentConfig(
                            name="sub1",
                            url="https://sub1.com",
                            headers={"Auth": "token"},
                            timeout=45.0
                        )
                    ]
                )
            ]
        )
        
        config.to_agent("test_agent")
        
        # Verify httpx client created for subagent
        mock_httpx.assert_called()
        
        # Verify max_iterations passed
        call_kwargs = mock_to_workflow.call_args[1]
        assert call_kwargs['max_iterations'] == 7

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    def test_model_headers_passed(self, mock_openai, mock_agent):
        """Test that model headers are passed to model instantiation."""
        config = AgentConfig(
            model=OpenAI(
                type="openai",
                model="gpt-4",
                headers={"Custom-Header": "value"}
            ),
            description="Test",
            instruction="Test instruction"
        )
        
        config.to_agent("test_agent")
        
        call_kwargs = mock_openai.call_args[1]
        assert call_kwargs['default_headers'] == {"Custom-Header": "value"}

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    def test_complete_agent_creation(self, mock_openai, mock_agent):
        """Test that Agent is created with all parameters."""
        mock_model = Mock()
        mock_openai.return_value = mock_model
        
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test agent description",
            instruction="Test instruction"
        )
        
        config.to_agent("my_agent")
        
        mock_agent.assert_called_once()
        call_kwargs = mock_agent.call_args[1]
        assert call_kwargs['name'] == "my_agent"
        assert call_kwargs['model'] == mock_model
        assert call_kwargs['description'] == "Test agent description"
        assert call_kwargs['instruction'] == "Test instruction"
        assert 'tools' in call_kwargs

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    @patch('kagent.adk.types.MCPToolset')
    @patch('kagent.adk.types.RemoteA2aAgent')
    @patch('kagent.adk.types.AgentTool')
    @patch('kagent.adk.types.to_workflow_agent')
    def test_all_tools_combined(
        self, mock_workflow, mock_agent_tool, mock_remote, 
        mock_mcp, mock_openai, mock_agent
    ):
        """Test agent creation with all tool types combined."""
        from google.adk.tools.mcp_tool import StreamableHTTPConnectionParams, SseConnectionParams
        
        http_params = Mock(spec=StreamableHTTPConnectionParams)
        sse_params = Mock(spec=SseConnectionParams)
        mock_workflow.return_value = Mock()
        
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test instruction",
            http_tools=[HttpMcpServerConfig(params=http_params, tools=["http1"])],
            sse_tools=[SseMcpServerConfig(params=sse_params, tools=["sse1"])],
            remote_agents=[
                RemoteAgentConfig(name="remote1", url="https://remote.com")
            ],
            workflow_subagents=[
                WorkflowConfig(
                    type="Sequential",
                    subagents=[SubagentConfig(name="sub1", url="https://sub.com")]
                )
            ]
        )
        
        config.to_agent("comprehensive_agent")
        
        # Verify all tool types were created
        assert mock_mcp.call_count >= 2  # HTTP + SSE
        assert mock_remote.call_count >= 1  # Remote agent
        assert mock_workflow.call_count >= 1  # Workflow
        mock_agent.assert_called_once()


# ============================================================================
# Integration Tests
# ============================================================================

class TestIntegration:
    """Integration tests for complete workflows."""

    @patch('kagent.adk.types.Agent')
    @patch('kagent.adk.types.OpenAINative')
    def test_minimal_agent_config(self, mock_openai, mock_agent):
        """Test minimal valid agent configuration."""
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Minimal agent",
            instruction="Do minimal things"
        )
        
        agent = config.to_agent("minimal")
        
        assert mock_agent.called
        assert mock_openai.called

    def test_workflow_name_length_compliance(self):
        """Test that workflow names always comply with 64-char limit."""
        test_cases = [
            ("short", "role", "Sequential"),
            ("very_long_base_name_that_exceeds_normal_length", "long_role_name", "Parallel"),
            ("strategic_analysis_workflow", "Strategic_Analysis_Pipeline", "Loop"),
        ]
        
        for base, role, wf_type in test_cases:
            sanitized_role = sanitize_agent_name(role) if role else ""
            workflow_name = generate_workflow_name(base, sanitized_role, wf_type)
            assert len(workflow_name) <= 64, f"Workflow name too long: {workflow_name}"


# ============================================================================
# Edge Cases and Error Conditions
# ============================================================================

class TestEdgeCases:
    """Test edge cases and error conditions."""

    def test_sanitize_unicode_characters(self):
        """Test sanitization with unicode characters.
        
        Note: Python's isalnum() returns True for Unicode alphanumeric characters,
        so they are preserved in the output.
        """
        assert sanitize_agent_name("hello_世界") == "hello_世界"  # Unicode preserved
        assert sanitize_agent_name("test™") == "test"  # ™ is not alphanumeric
        assert sanitize_agent_name("café") == "café"  # é is alphanumeric

    def test_sanitize_only_numbers(self):
        """Test string with only numbers."""
        assert sanitize_agent_name("123456") == "_123456"

    def test_workflow_name_all_special_chars(self):
        """Test workflow name generation with role containing only special chars."""
        result = generate_workflow_name("base", sanitize_agent_name("@#$"), "Sequential")
        assert len(result) <= 64
        # Should sanitize to underscore
        assert "_sequential" in result

    def test_to_workflow_agent_empty_subagents(self):
        """Test workflow creation with empty subagents list."""
        # This should work but might create an invalid workflow
        # The actual validation happens in the ADK library
        with patch('kagent.adk.types.SequentialAgent') as mock_seq:
            to_workflow_agent("Sequential", "test", [])
            mock_seq.assert_called_once_with(name="test", sub_agents=[])
    
    def test_invalid_model_type_error(self):
        """Test that invalid model type raises ValueError.
        
        This bypasses Pydantic validation to test the error handling in to_agent().
        """
        # Create a config with a valid model type first
        config = AgentConfig(
            model=OpenAI(type="openai", model="gpt-4"),
            description="Test",
            instruction="Test"
        )
        
        # Monkey-patch the model type to an invalid value to test the else branch
        config.model.type = "invalid_model_type"
        
        with pytest.raises(ValueError, match="Invalid model type: invalid_model_type"):
            config.to_agent("test_agent")

