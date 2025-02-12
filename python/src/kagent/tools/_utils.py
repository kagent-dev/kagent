from typing import Any, Dict, Generic, Optional, Tuple, Type, TypeVar, cast

from autogen_core import CancellationToken, Component
from autogen_core.tools import BaseTool, FunctionTool
from pydantic import BaseModel

TConfig = TypeVar("TConfig", bound=BaseModel)

def create_typed_fn_tool(
    fn_tool: FunctionTool,
    override_provider: str,
    class_name: str,
    config_type: Optional[Type[BaseModel]] = None
) -> Tuple[Type[BaseTool], Type[BaseModel]]:
    """Creates a concrete typed fn tool class from a function tool."""

    class EmptyConfig(BaseModel):
        pass

    ToolConfig = config_type or EmptyConfig

    class Tool(BaseTool, Component[ToolConfig], Generic[TConfig]):
        component_provider_override = override_provider
        component_type = "tool"
        component_config_schema = ToolConfig

        def __init__(self, config: Optional[BaseModel] = None):
            if config_type is None:
                self._config = EmptyConfig()
            else:
                # Validate config type if provided
                if config is not None and not isinstance(config, config_type):
                    raise TypeError(
                        f"Config must be of type {config_type.__name__}, "
                        f"got {type(config).__name__}"
                    )
                self._config = config or ToolConfig()

            super().__init__(
                name=fn_tool.name,
                description=fn_tool.description,
                args_type=fn_tool.args_type(),
                return_type=fn_tool.return_type(),
            )
            self.fn_tool = fn_tool

        async def run(
            self, args: Dict[str, Any], cancellation_token: CancellationToken
        ) -> Any:
            fn_params = self.fn_tool.parameters()
            if "config" in fn_params and not isinstance(self._config, EmptyConfig):
                args["config"] = self._config
            return await self.fn_tool.run(args, cancellation_token)

        def _to_config(self) -> BaseModel:
            return self._config

        @classmethod
        def _from_config(cls, config: BaseModel) -> BaseTool:
            return cast(BaseTool, cls(config))

    # Set the class name dynamically
    Tool.__name__ = class_name
    return Tool, ToolConfig
