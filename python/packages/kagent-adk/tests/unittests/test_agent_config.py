# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import json

import pytest

from kagent.adk.types import AgentConfig, OpenAI


class TestAgentConfigMaxPayloadSize:
    """Tests for AgentConfig max_payload_size field."""

    def test_agent_config_with_max_payload_size(self):
        """Test AgentConfig parsing with max_payload_size."""
        config_dict = {
            "model": {
                "type": "openai",
                "model": "gpt-4o",
                "base_url": "",
            },
            "description": "Test agent",
            "instruction": "You are a helpful assistant.",
            "max_payload_size": 50 * 1024 * 1024,  # 50MB
        }

        config = AgentConfig.model_validate(config_dict)

        assert config.max_payload_size == 50 * 1024 * 1024
        assert config.description == "Test agent"
        assert config.instruction == "You are a helpful assistant."

    def test_agent_config_without_max_payload_size(self):
        """Test AgentConfig parsing without max_payload_size (backward compatibility)."""
        config_dict = {
            "model": {
                "type": "openai",
                "model": "gpt-4o",
                "base_url": "",
            },
            "description": "Test agent",
            "instruction": "You are a helpful assistant.",
        }

        config = AgentConfig.model_validate(config_dict)

        assert config.max_payload_size is None
        assert config.description == "Test agent"

    def test_agent_config_max_payload_size_none(self):
        """Test AgentConfig parsing with max_payload_size explicitly set to None."""
        config_dict = {
            "model": {
                "type": "openai",
                "model": "gpt-4o",
                "base_url": "",
            },
            "description": "Test agent",
            "instruction": "You are a helpful assistant.",
            "max_payload_size": None,
        }

        config = AgentConfig.model_validate(config_dict)

        assert config.max_payload_size is None

    def test_agent_config_json_serialization(self):
        """Test that AgentConfig can be serialized to JSON with max_payload_size."""
        config = AgentConfig(
            model=OpenAI(
                type="openai",
                model="gpt-4o",
                base_url="",
            ),
            description="Test agent",
            instruction="You are a helpful assistant.",
            max_payload_size=100 * 1024 * 1024,  # 100MB
        )

        json_str = config.model_dump_json()
        parsed = json.loads(json_str)

        assert parsed["max_payload_size"] == 100 * 1024 * 1024
        assert parsed["description"] == "Test agent"

    def test_agent_config_max_payload_size_zero_raises_error(self):
        """Test that AgentConfig validation rejects zero max_payload_size."""
        config_dict = {
            "model": {
                "type": "openai",
                "model": "gpt-4o",
                "base_url": "",
            },
            "description": "Test agent",
            "instruction": "You are a helpful assistant.",
            "max_payload_size": 0,
        }

        with pytest.raises(Exception):  # Pydantic validation error
            AgentConfig.model_validate(config_dict)

    def test_agent_config_max_payload_size_negative_raises_error(self):
        """Test that AgentConfig validation rejects negative max_payload_size."""
        config_dict = {
            "model": {
                "type": "openai",
                "model": "gpt-4o",
                "base_url": "",
            },
            "description": "Test agent",
            "instruction": "You are a helpful assistant.",
            "max_payload_size": -100,
        }

        with pytest.raises(Exception):  # Pydantic validation error
            AgentConfig.model_validate(config_dict)
