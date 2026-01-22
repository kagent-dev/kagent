"""Unit tests for AgentConfig.model_validate TLS field extraction.

These tests verify that TLS fields (tls_client_cert_path, insecure_tls_verify) are
correctly extracted from JSON and set on StreamableHTTPConnectionParams objects
during AgentConfig.model_validate, even though these fields are not part of the
Pydantic model definition.
"""

import pytest
from google.adk.tools.mcp_tool import StreamableHTTPConnectionParams

from kagent.adk.types import AgentConfig, HttpMcpServerConfig, OpenAI


def test_model_validate_extracts_tls_fields_from_http_tools():
    """Test that TLS fields are extracted from JSON and set on params objects."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp.example.com/mcp",
                    "headers": {},
                    "tls_client_cert_path": "/etc/ssl/certs/test-client",
                    "insecure_tls_verify": False,
                },
                "tools": ["tool1", "tool2"],
            }
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    # Verify TLS fields were set on params object
    assert agent_config.http_tools is not None
    assert len(agent_config.http_tools) == 1
    http_tool = agent_config.http_tools[0]
    assert isinstance(http_tool, HttpMcpServerConfig)
    assert isinstance(http_tool.params, StreamableHTTPConnectionParams)

    # Verify TLS fields are accessible via getattr (they're not Pydantic fields)
    assert getattr(http_tool.params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client"
    assert getattr(http_tool.params, "insecure_tls_verify", None) is False


def test_model_validate_extracts_only_client_cert_path():
    """Test that only tls_client_cert_path is extracted when insecure_tls_verify is not provided."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp.example.com/mcp",
                    "tls_client_cert_path": "/etc/ssl/certs/test-client",
                },
                "tools": ["tool1"],
            }
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    http_tool = agent_config.http_tools[0]
    assert getattr(http_tool.params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client"
    assert getattr(http_tool.params, "insecure_tls_verify", None) is None


def test_model_validate_extracts_only_insecure_tls_verify():
    """Test that only insecure_tls_verify is extracted when tls_client_cert_path is not provided."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp.example.com/mcp",
                    "insecure_tls_verify": True,
                },
                "tools": ["tool1"],
            }
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    http_tool = agent_config.http_tools[0]
    assert getattr(http_tool.params, "tls_client_cert_path", None) is None
    assert getattr(http_tool.params, "insecure_tls_verify", None) is True


def test_model_validate_handles_no_tls_fields():
    """Test that model_validate works correctly when no TLS fields are present."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp.example.com/mcp",
                    "headers": {},
                },
                "tools": ["tool1"],
            }
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    http_tool = agent_config.http_tools[0]
    # TLS fields should not be set (both None)
    assert getattr(http_tool.params, "tls_client_cert_path", None) is None
    assert getattr(http_tool.params, "insecure_tls_verify", None) is None


def test_model_validate_handles_no_http_tools():
    """Test that model_validate works correctly when http_tools is not present."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
    }

    agent_config = AgentConfig.model_validate(config_dict)

    assert agent_config.http_tools is None


def test_model_validate_handles_empty_http_tools():
    """Test that model_validate works correctly when http_tools is an empty list."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    assert agent_config.http_tools == []


def test_model_validate_handles_multiple_http_tools():
    """Test that TLS fields are extracted for multiple http_tools."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp1.example.com/mcp",
                    "tls_client_cert_path": "/etc/ssl/certs/test-client-1",
                    "insecure_tls_verify": False,
                },
                "tools": ["tool1"],
            },
            {
                "params": {
                    "url": "https://test-mcp2.example.com/mcp",
                    "tls_client_cert_path": "/etc/ssl/certs/test-client-2",
                    "insecure_tls_verify": True,
                },
                "tools": ["tool2"],
            },
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    assert agent_config.http_tools is not None
    assert len(agent_config.http_tools) == 2

    # Verify first http_tool
    http_tool_1 = agent_config.http_tools[0]
    assert getattr(http_tool_1.params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client-1"
    assert getattr(http_tool_1.params, "insecure_tls_verify", None) is False

    # Verify second http_tool
    http_tool_2 = agent_config.http_tools[1]
    assert getattr(http_tool_2.params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client-2"
    assert getattr(http_tool_2.params, "insecure_tls_verify", None) is True


def test_model_validate_handles_mixed_tls_configs():
    """Test that model_validate handles http_tools with mixed TLS configurations."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp1.example.com/mcp",
                    "tls_client_cert_path": "/etc/ssl/certs/test-client-1",
                },
                "tools": ["tool1"],
            },
            {
                "params": {
                    "url": "https://test-mcp2.example.com/mcp",
                    "insecure_tls_verify": True,
                },
                "tools": ["tool2"],
            },
            {
                "params": {
                    "url": "https://test-mcp3.example.com/mcp",
                },
                "tools": ["tool3"],
            },
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    assert len(agent_config.http_tools) == 3

    # First tool: only client cert
    assert getattr(agent_config.http_tools[0].params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client-1"
    assert getattr(agent_config.http_tools[0].params, "insecure_tls_verify", None) is None

    # Second tool: only insecure_tls_verify
    assert getattr(agent_config.http_tools[1].params, "tls_client_cert_path", None) is None
    assert getattr(agent_config.http_tools[1].params, "insecure_tls_verify", None) is True

    # Third tool: no TLS config
    assert getattr(agent_config.http_tools[2].params, "tls_client_cert_path", None) is None
    assert getattr(agent_config.http_tools[2].params, "insecure_tls_verify", None) is None


def test_model_validate_handles_none_tls_values():
    """Test that model_validate handles None values for TLS fields correctly."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp.example.com/mcp",
                    "tls_client_cert_path": None,
                    "insecure_tls_verify": None,
                },
                "tools": ["tool1"],
            }
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    http_tool = agent_config.http_tools[0]
    # When both are None, they should not be set (or set to None)
    # The condition checks "if tls_client_cert_path is not None or insecure_tls_verify is not None"
    # So if both are None, the fields won't be set
    # But we should verify the behavior is correct
    tls_client_cert_path = getattr(http_tool.params, "tls_client_cert_path", None)
    insecure_tls_verify = getattr(http_tool.params, "insecure_tls_verify", None)
    # Both should be None (not set, or set to None)
    assert tls_client_cert_path is None
    assert insecure_tls_verify is None


def test_model_validate_preserves_other_params():
    """Test that model_validate preserves other params fields while setting TLS fields."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp.example.com/mcp",
                    "headers": {"Authorization": "Bearer token123"},
                    "timeout": 10.0,
                    "sse_read_timeout": 60.0,
                    "terminate_on_close": False,
                    "tls_client_cert_path": "/etc/ssl/certs/test-client",
                    "insecure_tls_verify": False,
                },
                "tools": ["tool1"],
            }
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    http_tool = agent_config.http_tools[0]
    params = http_tool.params

    # Verify other params are preserved
    assert params.url == "https://test-mcp.example.com/mcp"
    assert params.headers == {"Authorization": "Bearer token123"}
    assert params.timeout == 10.0
    assert params.sse_read_timeout == 60.0
    assert params.terminate_on_close is False

    # Verify TLS fields are set
    assert getattr(params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client"
    assert getattr(params, "insecure_tls_verify", None) is False


def test_model_validate_works_with_sse_tools():
    """Test that model_validate works correctly when both http_tools and sse_tools are present."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp.example.com/mcp",
                    "tls_client_cert_path": "/etc/ssl/certs/test-client",
                },
                "tools": ["tool1"],
            }
        ],
        "sse_tools": [
            {
                "params": {
                    "url": "https://test-sse.example.com/mcp",
                },
                "tools": ["tool2"],
            }
        ],
    }

    agent_config = AgentConfig.model_validate(config_dict)

    # Verify http_tools TLS field is set
    assert getattr(agent_config.http_tools[0].params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client"

    # Verify sse_tools are present (TLS extraction for SSE is handled separately)
    assert agent_config.sse_tools is not None
    assert len(agent_config.sse_tools) == 1


def test_model_validate_handles_missing_params_key():
    """Test that model_validate handles http_tool_data without params key gracefully."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "tools": ["tool1"],  # Missing params key
            }
        ],
    }

    # This should raise a validation error from Pydantic because params is required
    with pytest.raises(Exception):  # Pydantic validation error
        AgentConfig.model_validate(config_dict)


def test_model_validate_handles_non_dict_params():
    """Test that model_validate handles params that is not a dict."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": "not-a-dict",  # Invalid params type
                "tools": ["tool1"],
            }
        ],
    }

    # This should raise a validation error from Pydantic
    with pytest.raises(Exception):  # Pydantic validation error
        AgentConfig.model_validate(config_dict)


def test_model_validate_round_trip():
    """Test that TLS fields survive a round-trip through model_validate."""
    config_dict = {
        "model": {
            "type": "openai",
            "model": "gpt-4",
        },
        "description": "Test agent",
        "instruction": "Test instruction",
        "http_tools": [
            {
                "params": {
                    "url": "https://test-mcp.example.com/mcp",
                    "tls_client_cert_path": "/etc/ssl/certs/test-client",
                    "insecure_tls_verify": True,
                },
                "tools": ["tool1"],
            }
        ],
    }

    # First validation
    agent_config_1 = AgentConfig.model_validate(config_dict)
    assert getattr(agent_config_1.http_tools[0].params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client"
    assert getattr(agent_config_1.http_tools[0].params, "insecure_tls_verify", None) is True

    # Convert back to dict and validate again
    config_dict_2 = agent_config_1.model_dump()
    # Note: TLS fields won't be in model_dump() because they're not Pydantic fields
    # So we need to manually add them back for the round-trip test
    config_dict_2["http_tools"][0]["params"]["tls_client_cert_path"] = "/etc/ssl/certs/test-client"
    config_dict_2["http_tools"][0]["params"]["insecure_tls_verify"] = True

    agent_config_2 = AgentConfig.model_validate(config_dict_2)
    assert getattr(agent_config_2.http_tools[0].params, "tls_client_cert_path", None) == "/etc/ssl/certs/test-client"
    assert getattr(agent_config_2.http_tools[0].params, "insecure_tls_verify", None) is True
