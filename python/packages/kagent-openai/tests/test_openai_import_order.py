import os
import subprocess
import sys


def test_a2a_sdk_spans_are_off_when_kagent_openai_loads_first():
    env = {key: value for key, value in os.environ.items() if not key.startswith("OTEL_")}
    code = "import kagent.openai, a2a.utils.telemetry as t; print(t.otel_enabled)"
    result = subprocess.run([sys.executable, "-c", code], env=env, capture_output=True, text=True, check=True)

    assert result.stdout.strip().splitlines()[-1] == "False"
