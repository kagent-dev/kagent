"""Ollama model implementation using the OpenAI-compatible API endpoint."""

from __future__ import annotations

import logging
import os
from functools import cached_property
from typing import Literal, Optional

from openai import AsyncOpenAI, DefaultAsyncHttpxClient

from ._openai import BaseOpenAI

logger = logging.getLogger(__name__)

# Ollama-specific options that have no OpenAI equivalent — log once and ignore
_UNSUPPORTED_OLLAMA_OPTIONS = frozenset(
    [
        "num_ctx",
        "num_batch",
        "num_gpu",
        "num_thread",
        "low_vram",
        "f16_kv",
        "vocab_only",
        "use_mmap",
        "use_mlock",
        "num_keep",
        "repeat_last_n",
        "repeat_penalty",
        "tfs_z",
        "typical_p",
        "mirostat",
        "mirostat_tau",
        "mirostat_eta",
        "penalize_newline",
        "stop",
        "numa",
        "main_gpu",
        "num_predict",
    ]
)


class KAgentOllamaLlm(BaseOpenAI):
    """Ollama model via the Ollama OpenAI-compatible API endpoint.

    The Ollama server host is read from the ``OLLAMA_API_BASE`` environment
    variable (set by the kagent controller from ModelConfig.ollama.host).
    Falls back to ``http://localhost:11434`` if the variable is not set.

    Ollama-specific options (e.g. ``num_ctx``) that have no OpenAI equivalent
    are logged as a warning and ignored. Standard options like ``temperature``
    and ``top_p`` are forwarded normally.
    """

    type: Literal["ollama"] = "ollama"

    @cached_property
    def _client(self) -> AsyncOpenAI:
        http_client = self._create_http_client()
        base = os.environ.get("OLLAMA_API_BASE", "http://localhost:11434").rstrip("/")
        if not base.endswith("/v1"):
            base = f"{base}/v1"
        if http_client:
            return AsyncOpenAI(
                base_url=base,
                api_key="ollama",  # Ollama doesn't require a real key
                default_headers=self.default_headers,
                timeout=self.timeout,
                http_client=http_client,
            )
        return AsyncOpenAI(
            base_url=base,
            api_key="ollama",
            default_headers=self.default_headers,
            timeout=self.timeout,
            http_client=DefaultAsyncHttpxClient(),
        )

    @classmethod
    def supported_models(cls) -> list[str]:
        return []  # Ollama models are dynamic; no fixed regex


def create_ollama_llm(
    model: str,
    options: dict[str, object] | None,
    extra_headers: dict[str, str],
    api_key_passthrough: bool | None,
) -> KAgentOllamaLlm:
    """Build a KAgentOllamaLlm from converted Ollama options.

    Extracts OpenAI-compatible params from the options dict and logs a
    warning for any Ollama-specific keys that cannot be forwarded.
    """
    openai_params: dict[str, object] = {}
    if options:
        for key, value in options.items():
            if key in _UNSUPPORTED_OLLAMA_OPTIONS:
                logger.warning(
                    "Ollama option '%s' has no OpenAI-compatible equivalent and will be ignored. "
                    "Configure it on the Ollama server side instead.",
                    key,
                )
            elif key == "temperature":
                openai_params["temperature"] = float(value)
            elif key == "top_p":
                openai_params["top_p"] = float(value)
            elif key == "seed":
                openai_params["seed"] = int(value)
            elif key == "top_k":
                # top_k not supported by OpenAI spec; log and skip
                logger.warning(
                    "Ollama option 'top_k' is not supported by the OpenAI-compatible API and will be ignored."
                )
            else:
                logger.warning(
                    "Unknown Ollama option '%s' will be ignored.",
                    key,
                )

    return KAgentOllamaLlm(
        model=model,
        default_headers=extra_headers or {},
        api_key_passthrough=api_key_passthrough,
        **openai_params,
    )
