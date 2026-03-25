from typing import Any

from langchain.agents.middleware import HumanInTheLoopMiddleware
from langchain.agents.middleware.human_in_the_loop import (
    ActionRequest,
    InterruptOnConfig,
    ReviewConfig,
)
from langchain.agents.middleware.types import (
    AgentState,
    ContextT,
)
from langchain_core.messages import ToolCall

from langgraph.runtime import Runtime


class KagentActionRequest(ActionRequest):
    id: str


class KAgentHumanInTheLoopMiddleware(HumanInTheLoopMiddleware):
    def __init__(
        self,
        interrupt_on: list[str] | dict[str, bool] | None = None,
        **kwargs: Any,
    ):
        super().__init__(interrupt_on=interrupt_on, **kwargs)

    def _create_action_and_config(
        self,
        tool_call: ToolCall,
        config: InterruptOnConfig,
        state: AgentState[Any],
        runtime: Runtime[ContextT],
    ) -> tuple[ActionRequest, ReviewConfig]:
        """Create an ActionRequest and ReviewConfig for a tool call."""
        tool_name = tool_call["name"]
        tool_args = tool_call["args"]

        # Generate description using the description field (str or callable)
        description_value = config.get("description")
        if callable(description_value):
            description = description_value(tool_call, state, runtime)
        elif description_value is not None:
            description = description_value
        else:
            description = f"{self.description_prefix}\n\nTool: {tool_name}\nArgs: {tool_args}"

        # Create ActionRequest with description
        action_request = KagentActionRequest(
            name=tool_name,
            args=tool_args,
            description=description,
            id=tool_call["id"],
        )

        # Create ReviewConfig
        # eventually can get tool information and populate args_schema from there
        review_config = ReviewConfig(
            action_name=tool_name,
            allowed_decisions=config["allowed_decisions"],
        )

        return action_request, review_config
