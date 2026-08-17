import os


class KAgentConfig:
    _url: str
    _grpc_url: str
    _name: str
    _namespace: str

    def __init__(
        self,
        url: str | None = None,
        grpc_url: str | None = None,
        name: str | None = None,
        namespace: str | None = None,
    ):
        resolved_url = url or os.getenv("KAGENT_URL")
        resolved_grpc_url = grpc_url or os.getenv("KAGENT_GRPC_URL")
        resolved_name = name or os.getenv("KAGENT_NAME")
        resolved_namespace = namespace or os.getenv("KAGENT_NAMESPACE")
        if not resolved_url:
            raise ValueError("KAGENT_URL environment variable is not set")
        if not resolved_grpc_url:
            raise ValueError("KAGENT_GRPC_URL environment variable is not set")
        if not resolved_name:
            raise ValueError("KAGENT_NAME environment variable is not set")
        if not resolved_namespace:
            raise ValueError("KAGENT_NAMESPACE environment variable is not set")
        self._url = resolved_url
        self._grpc_url = resolved_grpc_url
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
    def url(self):
        return self._url

    @property
    def kagent_url(self):
        return self._url

    @property
    def grpc_url(self):
        return self._grpc_url
