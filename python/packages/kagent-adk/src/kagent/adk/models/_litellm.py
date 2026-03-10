from __future__ import annotations

from typing import Optional

from google.adk.models.lite_llm import LiteLlm


class KAgentLiteLlm(LiteLlm):
    """LiteLlm subclass that supports API key passthrough."""

    api_key_passthrough: Optional[bool] = None

    def __init__(self, model: str, **kwargs):
        passthrough = kwargs.pop("api_key_passthrough", None)
        super().__init__(model=model, **kwargs)
        self.api_key_passthrough = passthrough

    def set_passthrough_key(self, token: str) -> None:
        self._additional_args["api_key"] = token
