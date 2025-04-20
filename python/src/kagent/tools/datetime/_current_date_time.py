import datetime

from autogen_core.tools import FunctionTool

from .._utils import create_typed_fn_tool


async def _current_date_time() -> str:
    """Returns the current date and time in ISO 8601 format."""
    return datetime.datetime.now().isoformat()


current_date_time = FunctionTool(
    _current_date_time,
    description="Returns the current date and time in ISO 8601 format.",
    name="current_date_time",
)

GetCurrentDateTime, GetCurrentDateTimeConfig = create_typed_fn_tool(
    current_date_time, "kagent.tools.datetime.GetCurrentDateTime", "GetCurrentDateTime"
)
