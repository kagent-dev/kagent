import importlib

import pytest
from kagent.core.telemetry._profiles import COMMON_INSTRUMENTORS


@pytest.mark.parametrize("client_module", ["anthropic._base_client", "openai._base_client", "google.genai._api_client"])
def test_model_sdk_http_library_is_instrumented(client_module):
    module = importlib.import_module(client_module)
    library = next(name for name in ("httpx2", "httpx") if hasattr(module, name))

    assert library in COMMON_INSTRUMENTORS
