# Copyright 2026 Google LLC
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

from unittest.mock import Mock

import pytest
from google.adk.models.llm_request import LlmRequest

from kagent.adk.tools.session_tool import SessionInfoTool


class TestSessionInfoTool:
    @pytest.mark.asyncio
    async def test_session_info_tool(self):
        tool = SessionInfoTool()

        context = Mock()
        context.session = Mock()
        context.session.id = "session-123"
        context.session.user_id = "user-456"
        context.session.app_name = "test-app"

        llm_request = LlmRequest()
        await tool.process_llm_request(tool_context=context, llm_request=llm_request)

        assert "session-123" in llm_request.config.system_instruction
        assert "user-456" in llm_request.config.system_instruction
        assert "test-app" in llm_request.config.system_instruction
        assert "get_session_info" not in llm_request.tools_dict

    @pytest.mark.asyncio
    async def test_session_info_tool_none_session(self):
        tool = SessionInfoTool()

        context = Mock()
        context.session = None

        llm_request = LlmRequest()
        await tool.process_llm_request(tool_context=context, llm_request=llm_request)

        assert llm_request.config.system_instruction is None
