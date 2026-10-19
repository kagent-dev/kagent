from dataclasses import dataclass

from ._conv import KagentRuntimeValues


@dataclass(frozen=True)
class Profile:
    instrumentors: tuple[str, ...]
    emits_invoke_agent: bool


PROFILES: dict[KagentRuntimeValues, Profile] = {
    KagentRuntimeValues.ADK_PYTHON: Profile(instrumentors=(), emits_invoke_agent=True),
    KagentRuntimeValues.LANGGRAPH: Profile(instrumentors=("langchain",), emits_invoke_agent=False),
    KagentRuntimeValues.OPENAI_AGENTS: Profile(instrumentors=("openai_agents", "openai"), emits_invoke_agent=True),
    KagentRuntimeValues.CREWAI: Profile(instrumentors=("openai", "anthropic"), emits_invoke_agent=False),
    KagentRuntimeValues.BYO: Profile(instrumentors=(), emits_invoke_agent=False),
}

COMMON_INSTRUMENTORS = ("httpx", "httpx2")


def profile_for(runtime: KagentRuntimeValues) -> Profile:
    try:
        return PROFILES[runtime]
    except KeyError:
        raise ValueError(f"runtime {runtime!r} is not a Python runtime") from None
