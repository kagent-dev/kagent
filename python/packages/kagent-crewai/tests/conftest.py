import os

# KAgentConfig() is evaluated at import time (a default argument in _a2a.py),
# and it requires these environment variables. Set harmless test values before
# the executor module is imported so collection does not fail.
os.environ.setdefault("KAGENT_URL", "http://localhost:8080")
os.environ.setdefault("KAGENT_NAME", "test")
os.environ.setdefault("KAGENT_NAMESPACE", "default")
