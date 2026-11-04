from google.protobuf.json_format import MessageToDict, ParseDict
from kagent.api.v1.interactions_pb2 import ToolResponse


def encode_tool_response(response: dict) -> ToolResponse:
    if isinstance(response.get("error"), str):
        return ParseDict({"error": response["error"]}, ToolResponse())
    if response.get("status") == "pending":
        return ParseDict({"pending": response}, ToolResponse())
    if "output" not in response:
        return ParseDict({"output": response}, ToolResponse())

    output = response["output"]
    failed = response.get("isError") is True
    content = output
    if isinstance(output, dict):
        failed = failed or output.get("isError") is True
        if output.get("structuredContent") is not None:
            return ParseDict({"error" if failed else "output": output["structuredContent"]}, ToolResponse())
        content = output.get("content")
    if (
        isinstance(content, list)
        and content
        and all(
            isinstance(item, dict) and item.get("type") == "text" and isinstance(item.get("text"), str)
            for item in content
        )
    ):
        output = "\n".join(item["text"] for item in content)
    return ParseDict({"error" if failed else "output": output}, ToolResponse())


def decode_tool_response(response: ToolResponse) -> dict:
    outcome = response.WhichOneof("outcome")
    if outcome is None:
        raise ValueError("canonical tool response requires an outcome")
    value = MessageToDict(getattr(response, outcome))
    if outcome == "output":
        return value if isinstance(value, dict) else {"output": value}
    if outcome == "error":
        return {"error": value}
    return value if isinstance(value, dict) else {"status": "pending", "value": value}
