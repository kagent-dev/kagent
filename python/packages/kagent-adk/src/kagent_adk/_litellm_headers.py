import json
import os
from typing import Any, Dict

try:
	import httpx  # type: ignore
except Exception:  # pragma: no cover
	httpx = None  # type: ignore


def parse_headers_env() -> Dict[str, str]:
	raw = os.getenv("KAGENT_MODEL_DEFAULT_HEADERS", "")
	if not raw:
		return {}
	try:
		data: Any = json.loads(raw)
		return data if isinstance(data, dict) else {}
	except Exception:
		return {}
