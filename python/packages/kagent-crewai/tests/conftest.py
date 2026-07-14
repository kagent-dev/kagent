import os

_DEFAULTS = {
    "KAGENT_URL": "http://localhost:8080",
    "KAGENT_NAME": "test-agent",
    "KAGENT_NAMESPACE": "default",
}

_ORIGINAL_ENV: dict[str, str | None] = {}


def pytest_configure(config):
    # The kagent.crewai package imports KAgentApp at module load, which constructs a
    # KAgentConfig and requires these environment variables to be present. Set safe
    # defaults so the package can be imported in unit tests without a running backend.
    for key, value in _DEFAULTS.items():
        if key in os.environ:
            _ORIGINAL_ENV[key] = os.environ[key]
        else:
            _ORIGINAL_ENV[key] = None
            os.environ[key] = value


def pytest_unconfigure(config):
    # Restore environment so these defaults don't leak into other test suites.
    for key, original in _ORIGINAL_ENV.items():
        if original is None:
            os.environ.pop(key, None)
        else:
            os.environ[key] = original
