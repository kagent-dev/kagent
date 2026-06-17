"""Materialize Agent Substrate secret-backed configuration from environment variables.

On Agent Substrate the ActorTemplate cannot mount the agent config as files; instead the
config is injected as secret-backed environment variables and the running process must write
them to the on-disk paths the ADK loads from at startup. This mirrors the Go ADK's
``MaterializeFromEnv`` (see ``go/adk/pkg/config/config_materialize.go``): the environment value
is written verbatim (raw, not base64-encoded) to the destination file.

When the environment variables are absent (the normal Kubernetes Deployment path, where the
config is mounted as a volume) this is a no-op.
"""

import logging
import os

logger = logging.getLogger(__name__)

# Environment variables injected by the substrate ActorTemplate, keyed to the file name the
# ADK loads from within the config directory.
_ENV_TO_CONFIG_FILE = {
    "KAGENT_CONFIG_JSON": "config.json",
    "KAGENT_AGENT_CARD_JSON": "agent-card.json",
    "KAGENT_SRT_SETTINGS_JSON": "srt-settings.json",
}

# The bearer token is materialized to a fixed path outside the config dir, matching the Go ADK.
_KAGENT_TOKEN_ENV = "KAGENT_TOKEN"
_KAGENT_TOKEN_PATH = "/var/run/secrets/tokens/kagent-token"


def _materialize_env_to_file(env_key: str, path: str) -> bool:
    """Write the raw value of ``env_key`` to ``path`` (0600). Returns True if written."""
    value = os.getenv(env_key, "").strip()
    if not value:
        return False
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(value)
    os.chmod(path, 0o600)
    return True


def materialize_from_env(config_dir: str) -> None:
    """Write substrate secret-backed env vars to the paths the ADK loads from.

    No-op for any variable that is unset, so the volume-mounted Deployment path is unaffected.
    """
    for env_key, filename in _ENV_TO_CONFIG_FILE.items():
        if _materialize_env_to_file(env_key, os.path.join(config_dir, filename)):
            logger.info("Materialized %s from %s", filename, env_key)
    _materialize_env_to_file(_KAGENT_TOKEN_ENV, _KAGENT_TOKEN_PATH)
