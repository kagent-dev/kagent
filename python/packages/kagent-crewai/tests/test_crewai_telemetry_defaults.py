import os
import subprocess
import sys


def test_crewai_telemetry_is_off_when_kagent_crewai_loads_first():
    env = {key: value for key, value in os.environ.items() if not key.startswith(("CREWAI_", "OTEL_"))}
    code = "import kagent.crewai; from crewai.telemetry import Telemetry; print(Telemetry().ready)"
    result = subprocess.run([sys.executable, "-c", code], env=env, capture_output=True, text=True, check=True)

    assert result.stdout.strip().splitlines()[-1] == "False"
