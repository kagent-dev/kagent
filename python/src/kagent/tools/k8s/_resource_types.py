from enum import Enum


class ResourceTypes(Enum):
    # Istio
    ISTIO_AUTH_POLICY = "auth_policy"
    ISTIO_GATEWAY = "gateway"
    ISTIO_PEER_AUTHENTICATION = "peer_authentication"
    ISTIO_VIRTUAL_SERVICE = "virtual_service"

    # Linkerd
    LINKERD_AUTH_POLICY = "auth_policy"
    LINKERD_GATEWAY = "gateway"
    LINKERD_PEER_AUTHENTICATION = "peer_authentication"
    LINKERD_VIRTUAL_SERVICE = "virtual_service"

    # Argo
    ARGO_ROLLOUT = "rollout"
    ARGO_ANALYSIS_TEMPLATE = "analysis_template"

    # Gateway API
    GWAPI_GATEWAY = "gateway"
    GWAPI_GATEWAY_CLASS = "gateway_class"
    GWAPI_GRPC_ROUTE = "grpc_route"
    GWAPI_HTTP_ROUTE = "http_route"
    GWAPI_REFERENCE_GRANT = "reference_grant"
