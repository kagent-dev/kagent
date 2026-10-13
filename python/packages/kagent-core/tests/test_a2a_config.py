import pytest

from kagent.core.a2a import get_a2a_max_content_length


@pytest.mark.parametrize("value,expected", [(None, 10 * 1024 * 1024), (0, None), (50 * 1024 * 1024, 50 * 1024 * 1024)])
def test_request_limit_is_explicit(value, expected):
    assert get_a2a_max_content_length(value) == expected


def test_negative_request_limit_is_rejected():
    with pytest.raises(ValueError):
        get_a2a_max_content_length(-1)
