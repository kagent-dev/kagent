import os


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
    ):
        resolved_api_url = api_url or os.getenv("KAGENT_API_URL")
        resolved_gateway_url = gateway_url or os.getenv("KAGENT_GATEWAY_URL")
        resolved_name = name or os.getenv("KAGENT_NAME")
        resolved_namespace = namespace or os.getenv("KAGENT_NAMESPACE")
        if not resolved_api_url:
            raise ValueError("KAGENT_API_URL environment variable is not set")
        if not resolved_gateway_url:
            raise ValueError("KAGENT_GATEWAY_URL environment variable is not set")
        if not resolved_name:
            raise ValueError("KAGENT_NAME environment variable is not set")
        if not resolved_namespace:
            raise ValueError("KAGENT_NAMESPACE environment variable is not set")
        self._api_url = resolved_api_url
        self._gateway_url = resolved_gateway_url
        self._name = resolved_name
        self._namespace = resolved_namespace

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
