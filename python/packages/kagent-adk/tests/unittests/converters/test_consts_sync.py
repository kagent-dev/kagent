"""Verify kagent-core A2A constants stay in sync with upstream google-adk.

kagent-core defines these constants locally to avoid depending on google-adk.
This test ensures the values match the upstream definitions.
"""

import pytest
from google.adk.a2a.converters import part_converter as upstream

from kagent.core.a2a import _consts as local

# Each tuple is (constant_name, local_value, upstream_value).
_SYNCED_CONSTANTS = [
    (
        "A2A_DATA_PART_METADATA_TYPE_KEY",
        local.A2A_DATA_PART_METADATA_TYPE_KEY,
        upstream.A2A_DATA_PART_METADATA_TYPE_KEY,
    ),
    (
        "A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY",
        local.A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY,
        upstream.A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY,
    ),
    (
        "A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL",
        local.A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
        upstream.A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL,
    ),
    (
        "A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE",
        local.A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
        upstream.A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE,
    ),
    (
        "A2A_DATA_PART_METADATA_TYPE_CODE_EXECUTION_RESULT",
        local.A2A_DATA_PART_METADATA_TYPE_CODE_EXECUTION_RESULT,
        upstream.A2A_DATA_PART_METADATA_TYPE_CODE_EXECUTION_RESULT,
    ),
    (
        "A2A_DATA_PART_METADATA_TYPE_EXECUTABLE_CODE",
        local.A2A_DATA_PART_METADATA_TYPE_EXECUTABLE_CODE,
        upstream.A2A_DATA_PART_METADATA_TYPE_EXECUTABLE_CODE,
    ),
]


@pytest.mark.parametrize("name,local_val,upstream_val", _SYNCED_CONSTANTS, ids=[t[0] for t in _SYNCED_CONSTANTS])
def test_constant_matches_upstream(name: str, local_val: str, upstream_val: str) -> None:
    assert local_val == upstream_val, (
        f"kagent-core constant {name} = {local_val!r} does not match "
        f"upstream google-adk value {upstream_val!r}. "
        f"Update the value in kagent-core/src/kagent/core/a2a/_consts.py."
    )
