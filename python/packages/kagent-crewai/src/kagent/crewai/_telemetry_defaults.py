"""Settings kagent applies before crewai and a2a are imported."""

import os

from kagent.core.telemetry import _defaults

os.environ.setdefault("CREWAI_DISABLE_TELEMETRY", "true")
