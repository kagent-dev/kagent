from typing import Dict, List

from autogen_core import (
    ComponentModel,
)
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from ...toolservermanager import ToolServerManager

router = APIRouter()


class GetServerToolsRequest(BaseModel):
    server: ComponentModel


class NamedTool(BaseModel):
    name: str
    component: Dict


class GetServerToolsResponse(BaseModel):
    tools: List[NamedTool]


@router.post("/")
async def get_server_tools(
    request: GetServerToolsRequest,
) -> GetServerToolsResponse:
    # First check if server exists

    tsm = ToolServerManager()
    tools_dict: List[NamedTool] = []
    try:
        tools = await tsm.discover_tools(request.server)
        for tool in tools:
            # Generate a unique identifier for the tool from its component
            component_data = tool.dump_component().model_dump()

            # Check if the tool already exists based on id/name
            component_config = component_data.get("config", {})
            tool_config = component_config.get("tool", {})
            tool_name = tool_config.get("name", None)
            tools_dict.append(NamedTool(name=tool_name, component=component_data))

    except Exception as e:
        raise HTTPException(status_code=400, detail=f"Failed to get server tools: {str(e)}") from e

    return GetServerToolsResponse(tools=tools_dict)
