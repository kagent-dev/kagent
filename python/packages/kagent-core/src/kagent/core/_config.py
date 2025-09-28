import os

kagent_url = os.getenv("KAGENT_URL")
kagent_name = os.getenv("KAGENT_NAME")
kagent_namespace = os.getenv("KAGENT_NAMESPACE")


class KAgentConfig:
    _url: str
    _name: str
    _namespace: str

    def __init__(self):
        if not kagent_url:
            raise ValueError("KAGENT_URL environment variable is not set")
        if not kagent_name:
            raise ValueError("KAGENT_NAME environment variable is not set")
        if not kagent_namespace:
            raise ValueError("KAGENT_NAMESPACE environment variable is not set")
        self._url = kagent_url
        self._name = kagent_name
        self._namespace = kagent_namespace

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
