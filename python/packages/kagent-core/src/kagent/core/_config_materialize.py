"""Materialize Agent Substrate configuration from environment variables.

The ActorTemplate injects config JSON directly and credentials through SecretKeyRef environment
variables. Credential placeholders are expanded before writing the files loaded by the ADK.
This mirrors the Go ADK's ``MaterializeFromEnv``.

When the environment variables are absent this is a no-op.
"""

import json
import logging
import os

logger = logging.getLogger(__name__)

# Environment variables injected by the substrate ActorTemplate, keyed to the file name the
# ADK loads from within the config directory.
_ENV_TO_CONFIG_FILE = {
    "KAGENT_CONFIG_JSON": "config.json",
    "KAGENT_AGENT_CARD_JSON": "agent-card.json",
}

# The bearer token is materialized to a fixed path outside the config dir, matching the Go ADK.
_KAGENT_TOKEN_ENV = "KAGENT_TOKEN"
_KAGENT_TOKEN_PATH = "/var/run/secrets/tokens/kagent-token"


def _materialize_env_to_file(env_key: str, path: str) -> bool:
    """Write the raw value of ``env_key`` to ``path`` (0600). Returns True if written."""
    value = os.getenv(env_key, "").strip()
    if not value:
        return False
    if env_key == "KAGENT_CONFIG_JSON" and "__KAGENT_ENV[" in value:
        value = json.dumps(_expand_config_env(json.loads(value)), separators=(",", ":"))
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(value)
    os.chmod(path, 0o600)
    return True


def _expand_config_env(value):
    if isinstance(value, str) and value.startswith("__KAGENT_ENV[") and value.endswith("]__"):
        name = value[len("__KAGENT_ENV[") : -len("]__")]
        if name not in os.environ:
            raise ValueError(f"required environment variable {name} is not set")
        return os.environ[name]
    if isinstance(value, list):
        return [_expand_config_env(item) for item in value]
    if isinstance(value, dict):
        return {key: _expand_config_env(item) for key, item in value.items()}
    return value


def materialize_from_env(config_dir: str) -> None:
    """Write substrate config environment variables to the paths the ADK loads from.

    No-op for any variable that is unset, so the volume-mounted Deployment path is unaffected.
    """
    for env_key, filename in _ENV_TO_CONFIG_FILE.items():
        if _materialize_env_to_file(env_key, os.path.join(config_dir, filename)):
            logger.info("Materialized %s from %s", filename, env_key)
    # Best-effort: the token path (/var/run/secrets/tokens) may not exist or be writable for a
    # nonroot runtime. A missing token only degrades authenticated callbacks, so log and continue
    # rather than crash startup.
    try:
        _materialize_env_to_file(_KAGENT_TOKEN_ENV, _KAGENT_TOKEN_PATH)
    except OSError as e:
        logger.warning("Could not materialize %s to %s: %s", _KAGENT_TOKEN_ENV, _KAGENT_TOKEN_PATH, e)
