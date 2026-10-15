import json
from pathlib import Path

from protovalidate import validate

from ._config_materialize import materialize_from_env
from ._runtime_config import RuntimeConfig, parse_runtime_config


class KAgentConfig:
    _api_url: str
    _gateway_url: str
    _name: str
    _namespace: str

    def __init__(
        self,
        api_url: str | None = None,
        gateway_url: str | None = None,
        name: str | None = None,
        namespace: str | None = None,
        runtime: RuntimeConfig | None = None,
    ):
        self.runtime = runtime or RuntimeConfig()
        resolved_api_url = api_url if api_url is not None else self.runtime.control_plane_url
        resolved_gateway_url = gateway_url if gateway_url is not None else self.runtime.gateway_url
        resolved_name = name if name is not None else self.runtime.name
        resolved_namespace = namespace if namespace is not None else self.runtime.namespace
        if not resolved_api_url:
            raise ValueError("runtime.control_plane_url is required")
        if not resolved_gateway_url:
            raise ValueError("runtime.gateway_url is required")
        if not resolved_name:
            raise ValueError("runtime.name is required")
        if not resolved_namespace:
            raise ValueError("runtime.namespace is required")
        self._api_url = resolved_api_url
        self._gateway_url = resolved_gateway_url
        self._name = resolved_name
        self._namespace = resolved_namespace
        resolved = RuntimeConfig()
        resolved.CopyFrom(self.runtime)
        resolved.control_plane_url = resolved_api_url
        resolved.gateway_url = resolved_gateway_url
        resolved.name = resolved_name
        resolved.namespace = resolved_namespace
        validate(resolved)
        self.runtime = resolved

    @classmethod
    def from_file(cls, path: str | Path = "/config/config.json") -> "KAgentConfig":
        """Load root settings from the generated runtime document.

        Substrate's private JSON payload is materialized before loading, just as
        for the ADK. A standalone process can supply the same document on disk.
        """
        path = Path(path)
        materialize_from_env(str(path.parent))
        document = json.loads(path.read_text())
        return cls(runtime=parse_runtime_config(document["runtime"]))

    @property
    def name(self):
        return self._name.replace("-", "_")

    @property
    def namespace(self):
        return self._namespace.replace("-", "_")

    @property
    def app_name(self):
        return self.namespace + "__NS__" + self.name

    @property
    def api_url(self):
        return self._api_url

    @property
    def gateway_url(self):
        return self._gateway_url
