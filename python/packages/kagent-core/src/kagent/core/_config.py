from kagent.core.env import register_string

kagent_url = register_string(
    "KAGENT_URL",
    None,
    "Base URL for A2A communication with the kagent controller.",
    "agent-runtime",
)
kagent_name = register_string(
    "KAGENT_NAME",
    None,
    "Name of the agent. Injected into agent pods via the controller.",
    "agent-runtime",
)
kagent_namespace = register_string(
    "KAGENT_NAMESPACE",
    None,
    "Kubernetes namespace where the agent is deployed.",
    "agent-runtime",
)


class KAgentConfig:
    _url: str
    _name: str
    _namespace: str

    def __init__(self, url: str = None, name: str = None, namespace: str = None):
        if not kagent_url and not url:
            raise ValueError("KAGENT_URL environment variable is not set")
        if not kagent_name and not name:
            raise ValueError("KAGENT_NAME environment variable is not set")
        if not kagent_namespace and not namespace:
            raise ValueError("KAGENT_NAMESPACE environment variable is not set")
        self._url = kagent_url if not url else url
        self._name = kagent_name if not name else name
        self._namespace = kagent_namespace if not namespace else namespace

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
