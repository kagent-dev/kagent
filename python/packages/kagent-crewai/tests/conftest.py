import os

# The kagent.crewai package imports KAgentApp at module load, which constructs a
# KAgentConfig and requires these environment variables to be present. Set safe
# defaults so the package can be imported in unit tests without a running backend.
os.environ.setdefault("KAGENT_URL", "http://localhost:8080")
os.environ.setdefault("KAGENT_NAME", "test-agent")
os.environ.setdefault("KAGENT_NAMESPACE", "default")
