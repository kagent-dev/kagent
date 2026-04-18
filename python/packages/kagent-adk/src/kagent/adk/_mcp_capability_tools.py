from __future__ import annotations

import base64
import json
import logging
from typing import TYPE_CHECKING, Any

from google.adk.tools import BaseTool
from google.genai import types
from typing_extensions import override

if TYPE_CHECKING:
    from google.adk.models.llm_request import LlmRequest
    from google.adk.tools.tool_context import ToolContext

logger = logging.getLogger("kagent_adk." + __name__)


class _BaseKAgentMcpLoaderTool(BaseTool):
    def __init__(self, *, mcp_toolset: Any, name: str, description: str, server_label: str):
        super().__init__(name=name, description=description)
        self._mcp_toolset = mcp_toolset
        self._server_label = server_label

    def _latest_function_responses(self, llm_request: "LlmRequest") -> list[dict[str, Any]]:
        if not llm_request.contents:
            return []

        # Search backwards — other helpers may have appended content after
        # the function response block during the same process_llm_request pass.
        for content in reversed(llm_request.contents):
            if not content.parts:
                continue

            matching_responses: list[dict[str, Any]] = []
            has_any_function_response = False
            for part in content.parts:
                function_response = part.function_response
                if function_response is None:
                    continue
                has_any_function_response = True
                if function_response.name == self.name:
                    matching_responses.append(function_response.response or {})

            if has_any_function_response:
                return matching_responses

        return []

    def _block_to_part(self, block: dict[str, Any], fallback_name: str) -> types.Part:
        block_type = block.get("type")
        if block.get("text") is not None and block_type in {None, "text"}:
            return types.Part.from_text(text=block["text"])

        if block.get("blob") is not None and block_type is None:
            return self._binary_part_from_base64(
                payload=block["blob"],
                mime_type=block.get("mimeType") or "application/octet-stream",
                fallback_name=fallback_name,
            )

        if block_type in {"image", "audio"} and block.get("data") is not None:
            return self._binary_part_from_base64(
                payload=block["data"],
                mime_type=block.get("mimeType") or "application/octet-stream",
                fallback_name=fallback_name,
            )

        if block_type == "resource":
            resource = block.get("resource") or {}
            if resource.get("text") is not None:
                return types.Part.from_text(text=resource["text"])
            if resource.get("blob") is not None:
                return self._binary_part_from_base64(
                    payload=resource["blob"],
                    mime_type=resource.get("mimeType") or "application/octet-stream",
                    fallback_name=fallback_name,
                )
            return types.Part.from_text(text=f"[Resource content for {fallback_name} could not be rendered]")

        if block_type == "resource_link":
            return types.Part.from_text(text=json.dumps(block, indent=2, sort_keys=True))

        return types.Part.from_text(text=json.dumps(block, indent=2, sort_keys=True))

    def _binary_part_from_base64(self, payload: str, mime_type: str, fallback_name: str) -> types.Part:
        try:
            return types.Part.from_bytes(data=base64.b64decode(payload), mime_type=mime_type)
        except Exception:
            return types.Part.from_text(text=f"[Binary content for {fallback_name} could not be decoded]")


class LoadKAgentMcpResourceTool(_BaseKAgentMcpLoaderTool):
    def __init__(self, *, mcp_toolset: Any, name: str, server_label: str):
        super().__init__(
            mcp_toolset=mcp_toolset,
            name=name,
            server_label=server_label,
            description=(
                f"Loads named resources from the MCP server '{server_label}' into the current context. "
                "Call this before answering questions that depend on MCP resources."
            ),
        )

    def _get_declaration(self) -> types.FunctionDeclaration | None:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "resource_names": types.Schema(
                        type=types.Type.ARRAY,
                        description="The MCP resource names to load into context.",
                        items=types.Schema(type=types.Type.STRING),
                    )
                },
                required=["resource_names"],
            ),
        )

    @override
    async def run_async(self, *, args: dict[str, Any], tool_context: "ToolContext") -> Any:
        raw_resource_names = args.get("resource_names", [])
        if isinstance(raw_resource_names, str):
            raw_resource_names = [raw_resource_names]
        if not isinstance(raw_resource_names, (list, tuple)):
            raw_resource_names = []
        resource_names = [str(name) for name in raw_resource_names if name]
        return {
            "resource_names": resource_names,
            "status": "Requested MCP resources have been staged into the next model turn.",
        }

    @override
    async def process_llm_request(self, *, tool_context: "ToolContext", llm_request: "LlmRequest") -> None:
        await super().process_llm_request(tool_context=tool_context, llm_request=llm_request)
        await self._append_resource_catalog(tool_context=tool_context, llm_request=llm_request)
        await self._append_selected_resources(tool_context=tool_context, llm_request=llm_request)

    async def _append_resource_catalog(self, *, tool_context: "ToolContext", llm_request: "LlmRequest") -> None:
        try:
            resource_names = await self._mcp_toolset.list_resources(tool_context)
        except Exception as error:
            logger.warning("Failed to list MCP resources from %s: %s", self._server_label, error)
            return

        if not resource_names:
            return

        llm_request.append_instructions(
            [
                (
                    f"You have MCP resources available from server '{self._server_label}':\n"
                    f"{json.dumps(resource_names)}\n\n"
                    f"When the user asks about one of these resources, call `{self.name}` first "
                    "with the relevant resource name or names."
                )
            ]
        )

    async def _append_selected_resources(self, *, tool_context: "ToolContext", llm_request: "LlmRequest") -> None:
        responses = self._latest_function_responses(llm_request)
        if not responses:
            return

        for response in responses:
            for resource_name in response.get("resource_names", []):
                try:
                    contents = await self._mcp_toolset.read_resource(resource_name, tool_context)
                except Exception as error:
                    logger.warning(
                        "Failed to read MCP resource '%s' from %s: %s", resource_name, self._server_label, error
                    )
                    continue

                for content in contents:
                    llm_request.contents.append(
                        types.Content(
                            role="user",
                            parts=[
                                types.Part.from_text(
                                    text=f"MCP resource '{resource_name}' from server '{self._server_label}' is:"
                                ),
                                self._block_to_part(content, resource_name),
                            ],
                        )
                    )


class LoadKAgentMcpPromptTool(_BaseKAgentMcpLoaderTool):
    def __init__(self, *, mcp_toolset: Any, name: str, server_label: str):
        super().__init__(
            mcp_toolset=mcp_toolset,
            name=name,
            server_label=server_label,
            description=(
                f"Loads a named prompt from the MCP server '{server_label}' into the current context. "
                "Pass any required prompt arguments as string values."
            ),
        )

    def _get_declaration(self) -> types.FunctionDeclaration | None:
        return types.FunctionDeclaration(
            name=self.name,
            description=self.description,
            parameters=types.Schema(
                type=types.Type.OBJECT,
                properties={
                    "prompt_name": types.Schema(
                        type=types.Type.STRING,
                        description="The MCP prompt name to load.",
                    ),
                    "arguments": types.Schema(
                        type=types.Type.OBJECT,
                        description="Optional string arguments for the MCP prompt template.",
                    ),
                },
                required=["prompt_name"],
            ),
        )

    @override
    async def run_async(self, *, args: dict[str, Any], tool_context: "ToolContext") -> Any:
        raw_arguments = args.get("arguments") or {}
        if not isinstance(raw_arguments, dict):
            raw_arguments = {}

        arguments = {str(key): str(value) for key, value in raw_arguments.items()}
        prompt_name = str(args.get("prompt_name", "")).strip()
        return {
            "prompt_name": prompt_name,
            "arguments": arguments,
            "status": "Requested MCP prompt has been staged into the next model turn.",
        }

    @override
    async def process_llm_request(self, *, tool_context: "ToolContext", llm_request: "LlmRequest") -> None:
        await super().process_llm_request(tool_context=tool_context, llm_request=llm_request)
        await self._append_prompt_catalog(tool_context=tool_context, llm_request=llm_request)
        await self._append_selected_prompt(tool_context=tool_context, llm_request=llm_request)

    async def _append_prompt_catalog(self, *, tool_context: "ToolContext", llm_request: "LlmRequest") -> None:
        try:
            prompt_info = await self._mcp_toolset.list_prompt_info(tool_context)
        except Exception as error:
            logger.warning("Failed to list MCP prompts from %s: %s", self._server_label, error)
            return

        if not prompt_info:
            return

        prompt_catalog = []
        for prompt in prompt_info:
            prompt_catalog.append(
                {
                    "name": prompt.get("name"),
                    "description": prompt.get("description"),
                    "arguments": [
                        {
                            "name": argument.get("name"),
                            "description": argument.get("description"),
                            "required": argument.get("required"),
                        }
                        for argument in prompt.get("arguments", [])
                    ],
                }
            )

        llm_request.append_instructions(
            [
                (
                    f"You have MCP prompts available from server '{self._server_label}':\n"
                    f"{json.dumps(prompt_catalog, indent=2)}\n\n"
                    f"When a prompt is relevant, call `{self.name}` with `prompt_name` and any required string arguments "
                    "before composing your final answer."
                )
            ]
        )

    async def _append_selected_prompt(self, *, tool_context: "ToolContext", llm_request: "LlmRequest") -> None:
        responses = self._latest_function_responses(llm_request)
        if not responses:
            return

        for response in responses:
            prompt_name = str(response.get("prompt_name", "")).strip()
            if not prompt_name:
                continue

            raw_arguments = response.get("arguments") or {}
            if not isinstance(raw_arguments, dict):
                raw_arguments = {}
            arguments = {str(key): str(value) for key, value in raw_arguments.items()}

            try:
                prompt = await self._mcp_toolset.get_prompt(prompt_name, arguments, tool_context)
            except Exception as error:
                logger.warning("Failed to load MCP prompt '%s' from %s: %s", prompt_name, self._server_label, error)
                continue

            for index, message in enumerate(prompt.get("messages", []), start=1):
                content = message.get("content") or {}
                role = _map_mcp_prompt_role(message.get("role"))
                llm_request.contents.append(
                    types.Content(
                        role=role,
                        parts=[
                            types.Part.from_text(
                                text=(
                                    f"MCP prompt '{prompt_name}' from server '{self._server_label}' "
                                    f"returned message {index} with role '{message.get('role') or 'user'}':"
                                )
                            ),
                            self._block_to_part(content, f"{prompt_name}_{index}"),
                        ],
                    )
                )


def _map_mcp_prompt_role(role: Any) -> str:
    if role == "assistant":
        return "model"
    if role == "user":
        return "user"
    return "user"
