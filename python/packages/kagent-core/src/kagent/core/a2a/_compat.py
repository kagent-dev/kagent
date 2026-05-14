"""Compatibility helpers for a2a-sdk 1.x."""

_V03_TYPE_ALIAS_NAMES = (
    "AgentCard",
    "Artifact",
    "DataPart",
    "FilePart",
    "Message",
    "MessageSendConfiguration",
    "MessageSendParams",
    "Part",
    "PartBase",
    "Role",
    "Task",
    "TaskArtifactUpdateEvent",
    "TaskIdParams",
    "TaskQueryParams",
    "TaskState",
    "TaskStatus",
    "TaskStatusUpdateEvent",
    "TextPart",
    "TransportProtocol",
)


def install_v03_type_aliases(*, overwrite: bool = False) -> dict[str, object]:
    """Expose removed v0.3 Pydantic type aliases for transitive imports.

    google-adk still imports several v0.3 model names from ``a2a.types``.
    a2a-sdk 1.x keeps those models under ``a2a.compat.v0_3.types`` instead.
    """

    import sys
    import types

    import a2a.client as a2a_client
    import a2a.client.errors as a2a_client_errors
    import a2a.types as a2a_types
    from a2a.compat.v0_3 import types as a2a_v03_types

    originals = {}
    for name in _V03_TYPE_ALIAS_NAMES:
        if hasattr(a2a_types, name):
            originals[name] = getattr(a2a_types, name)
        if (overwrite or not hasattr(a2a_types, name)) and hasattr(a2a_v03_types, name):
            setattr(a2a_types, name, getattr(a2a_v03_types, name))

    if not hasattr(a2a_client_errors, "A2AClientHTTPError"):
        a2a_client_errors.A2AClientHTTPError = a2a_client_errors.A2AClientError

    if not hasattr(a2a_client, "ClientEvent"):
        a2a_client.ClientEvent = tuple

    if "a2a.client.middleware" not in sys.modules:
        middleware = types.ModuleType("a2a.client.middleware")
        middleware.ClientCallContext = a2a_client.ClientCallContext
        middleware.ClientCallInterceptor = a2a_client.ClientCallInterceptor

        def __getattr__(name: str) -> object:
            raise AttributeError(
                "a2a.client.middleware was removed in a2a-sdk 1.x; "
                f"KAgent only provides compatibility aliases for ClientCallContext "
                f"and ClientCallInterceptor, not {name!r}."
            )

        middleware.__getattr__ = __getattr__
        sys.modules["a2a.client.middleware"] = middleware

    return originals


def restore_a2a_type_aliases(originals: dict[str, object]) -> None:
    import a2a.types as a2a_types

    for name, value in originals.items():
        setattr(a2a_types, name, value)
