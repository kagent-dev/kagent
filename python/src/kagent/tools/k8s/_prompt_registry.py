import logging
from typing import Dict

from ._resource_types import ResourceTypes
from .argo import ANALYSIS_TEMPLATE_PROMPT, ROLLOUT_PROMPT
from .gateway_api import GATEWAY_CLASS_PROMPT, GRPC_ROUTE_PROMPT, HTTP_ROUTE_PROMPT, REFERENCE_GRANT_PROMPT
from .gateway_api import GATEWAY_PROMPT as GWAPI_GATEWAY_PROMPT
from .istio import AUTH_POLICY_PROMPT, GATEWAY_PROMPT, PEER_AUTHENTICATION_PROMPT, VIRTUAL_SERVICE_PROMPT
from .linkerd import AUTH_POLICY_PROMPT, GATEWAY_PROMPT, PEER_AUTHENTICATION_PROMPT, VIRTUAL_SERVICE_PROMPT

logger = logging.getLogger(__name__)

PROMPT_REGISTRY: Dict[ResourceTypes, str] = {
    ResourceTypes.ISTIO_AUTH_POLICY: AUTH_POLICY_PROMPT,
    ResourceTypes.ISTIO_GATEWAY: GATEWAY_PROMPT,
    ResourceTypes.ISTIO_PEER_AUTHENTICATION: PEER_AUTHENTICATION_PROMPT,
    ResourceTypes.ISTIO_VIRTUAL_SERVICE: VIRTUAL_SERVICE_PROMPT,
    ResourceTypes.LINKERD_AUTH_POLICY: AUTH_POLICY_PROMPT,
    ResourceTypes.LINKERD_GATEWAY: GATEWAY_PROMPT,
    ResourceTypes.LINKERD_PEER_AUTHENTICATION: PEER_AUTHENTICATION_PROMPT,
    ResourceTypes.LINKERD_VIRTUAL_SERVICE: VIRTUAL_SERVICE_PROMPT,
    ResourceTypes.ARGO_ROLLOUT: ROLLOUT_PROMPT,
    ResourceTypes.ARGO_ANALYSIS_TEMPLATE: ANALYSIS_TEMPLATE_PROMPT,
    ResourceTypes.GWAPI_GATEWAY: GWAPI_GATEWAY_PROMPT,
    ResourceTypes.GWAPI_GATEWAY_CLASS: GATEWAY_CLASS_PROMPT,
    ResourceTypes.GWAPI_GRPC_ROUTE: GRPC_ROUTE_PROMPT,
    ResourceTypes.GWAPI_HTTP_ROUTE: HTTP_ROUTE_PROMPT,
    ResourceTypes.GWAPI_REFERENCE_GRANT: REFERENCE_GRANT_PROMPT,
}


def get_system_prompt(resource_type: ResourceTypes) -> str:
    """
    Retrieve the system prompt for the specified resource type.

    Args:
        resource_type: The CRD resource type

    Returns:
        The corresponding system prompt

    Raises:
        ValueError: If the resource type is not supported
    """
    try:
        return PROMPT_REGISTRY[resource_type]
    except KeyError:
        logger.error(f"Unsupported resource type: {resource_type}")
        raise ValueError(f"Unsupported resource type: {resource_type}") from KeyError
