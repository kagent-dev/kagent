from typing import Annotated, List, Optional

from autogen_core.tools import FunctionTool

from .._utils import create_typed_fn_tool
from ..common import run_command


async def _ztunnel_config(
    ns: Annotated[Optional[str], "The namespace of the pod to get proxy configuration for"],
    config_type: Annotated[
        Optional[str],
        "The type of configuration to get, the allowed values are: all, bootstrap, cluster, ecds, listener, log, route, secret",
    ] = "all",
) -> str:
    return _run_istioctl_command(f"ztunnel-config {config_type} {'-n ' + ns if ns else ''}")


async def _waypoint_status(
    name: Annotated[str, "Name of the waypoint to get status for"],
    ns: Annotated[str, "Namespace of the waypoint to get status for"],
) -> str:
    return _run_istioctl_command(f"waypoint status {name if name else ''} {f'-n {ns} ' if ns else ''}")


async def _list_waypoints(
    ns: Annotated[Optional[str], "Namespace to list waypoints for"],
    all_namespaces: Annotated[Optional[bool], "List waypoints for all namespaces"] = False,
) -> str:
    return _run_istioctl_command(f"waypoint list {f'-n {ns} ' if ns else ''} {'-A' if all_namespaces else ''}")


async def _generate_waypoint(
    ns: Annotated[str, "Namespace to generate the waypoint for"],
    name: Annotated[Optional[str], "Name of the waypoint to generate"] = "waypoint",
    traffic_type: Annotated[str, "Traffic type for the waypoint"] = "all",
) -> str:
    return _run_istioctl_command(
        f"waypoint generate {name if name else ''} {f'-n {ns} ' if ns else ''} {f'--for {traffic_type}' if traffic_type else ''}"
    )


async def _delete_waypoint(
    name: Annotated[List[str], "Name of the waypoints to delete"],
    ns: Annotated[str, "Namespace to delete the waypoint from"],
    all: Annotated[bool, "Delete all waypoints in the namespace"],
) -> str:
    return _run_istioctl_command(
        f"waypoint delete {' '.join(name)} {f'-n {ns} ' if ns else ''} {'--all' if all else ''}"
    )


async def _apply_waypoint(
    ns: Annotated[str, "Namespace to apply the waypoint to"],
    enroll_namespace: Annotated[bool, "If set, the namespace will be labeled with the waypoint name"],
) -> str:
    return _run_istioctl_command(
        f"waypoint apply {'-n ' + ns if ns else ''} {'--enroll-namespace' if enroll_namespace else ''}"
    )


async def _remote_clusters() -> str:
    return _run_istioctl_command("remote-clusters")


async def _analyze_cluster_configuration() -> str:
    return _run_istioctl_command("analyze")


async def _proxy_status(
    pod_name: Annotated[Optional[str], "The name of the pod to get Envoy proxy status for"],
    ns: Annotated[Optional[str], "The namespace of the pod to get Envoy proxy status for"],
) -> str:
    return _run_istioctl_command(f"proxy-status {'-n ' + ns if ns else ''} {pod_name if pod_name else ''}")


async def _install_istio(
    profile: Annotated[
        str, "Istio configuration profile to install, the allowed values are: ambient, default, demo, minimal, empty"
    ],
) -> str:
    return _run_istioctl_command(f"install --set profile={profile} -y")


async def _proxy_config(
    pod_name: Annotated[str, "The name of the pod to get proxy configuration for"],
    ns: Annotated[Optional[str], "The namespace of the pod to get proxy configuration for"],
    config_type: Annotated[
        Optional[str],
        "The type of configuration to get, the allowed values are: all, bootstrap, cluster, ecds, listener, log, route, secret",
    ] = "all",
) -> str:
    return _run_istioctl_command(f"proxy-config {config_type} {pod_name}{'.' + ns if ns else ''}")


async def _generate_manifest(
    profile: Annotated[
        str,
        "Istio configuration profile to generate manifest for, the allowed values are: ambient, default, demo, minimal, empty",
    ],
) -> str:
    return _run_istioctl_command(f"manifest generate --set profile={profile}")


async def _version() -> str:
    return _run_istioctl_command("version")


version = FunctionTool(
    _version,
    description="Returns the Istio CLI client version, control plane and the data plane versions and number of proxies running in the cluster. If Istio is not installed, it will return the Istio CLI client version.",
    name="version",
)

Version, VersionConfig = create_typed_fn_tool(version, "kagent.tools.istio.Version", "Version")

ztunnel_config = FunctionTool(
    _ztunnel_config,
    description="Get ztunnel configuration",
    name="ztunnel_config",
)

ZTunnelConfig, ZTunnelConfigConfig = create_typed_fn_tool(
    ztunnel_config, "kagent.tools.istio.ZTunnelConfig", "ZTunnelConfig"
)

waypoint_status = FunctionTool(
    _waypoint_status,
    description="Get status of a waypoint",
    name="waypoint_status",
)

WaypointStatus, WaypointStatusConfig = create_typed_fn_tool(
    waypoint_status, "kagent.tools.istio.WaypointStatus", "WaypointStatus"
)

list_waypoints = FunctionTool(
    _list_waypoints,
    description="List managed waypoint configurations in the cluster",
    name="list_waypoints",
)

ListWaypoints, ListWaypointsConfig = create_typed_fn_tool(
    list_waypoints, "kagent.tools.istio.ListWaypoints", "ListWaypoints"
)

generate_waypoint = FunctionTool(
    _generate_waypoint,
    description="Generate a waypoint configuration as YAML",
    name="generate_waypoint",
)

GenerateWaypoint, GenerateWaypointConfig = create_typed_fn_tool(
    generate_waypoint, "kagent.tools.istio.GenerateWaypoint", "GenerateWaypoint"
)

delete_waypoint = FunctionTool(
    _delete_waypoint,
    description="Delete a waypoint configuration from a cluster",
    name="delete_waypoint",
)

DeleteWaypoint, DeleteWaypointConfig = create_typed_fn_tool(
    delete_waypoint, "kagent.tools.istio.DeleteWaypoint", "DeleteWaypoint"
)

apply_waypoint = FunctionTool(
    _apply_waypoint, description="Apply a waypoint configuration to a cluster", name="apply_waypoint"
)

ApplyWaypoint, ApplyWaypointConfig = create_typed_fn_tool(
    apply_waypoint, "kagent.tools.istio.ApplyWaypoint", "ApplyWaypoint"
)

remote_clusters = FunctionTool(
    _remote_clusters,
    description="Lists the remote clusters each istiod instance is connected to",
    name="remote_clusters",
)

RemoteClusters, RemoteClustersConfig = create_typed_fn_tool(
    remote_clusters, "kagent.tools.istio.RemoteClusters", "RemoteClusters"
)

proxy_status = FunctionTool(
    _proxy_status,
    description="Get Envoy proxy status for a pod, retrieves last sent and last acknowledged xDS sync from Istiod to each Envoy in the mesh",
    name="proxy_status",
)

ProxyStatus, ProxyStatusConfig = create_typed_fn_tool(proxy_status, "kagent.tools.istio.ProxyStatus", "ProxyStatus")

generate_manifest = FunctionTool(
    _generate_manifest,
    description="Generates an Istio install manifest and outputs to the console by default.",
    name="generate_manifest",
)

GenerateManifest, GenerateManifestConfig = create_typed_fn_tool(
    generate_manifest, "kagent.tools.istio.GenerateManifest", "GenerateManifest"
)

install_istio = FunctionTool(
    _install_istio,
    description="Install Istio",
    name="install_istio",
)

InstallIstio, InstallIstioConfig = create_typed_fn_tool(install_istio, "kagent.tools.istio.Install", "Install")

analyze_cluster_configuration = FunctionTool(
    _analyze_cluster_configuration,
    description="Analyzes live cluster configuration",
    name="analyze_cluster_configuration",
)

AnalyzeClusterConfig, AnalyzeClusterConfigConfig = create_typed_fn_tool(
    analyze_cluster_configuration, "kagent.tools.istio.AnalyzeClusterConfig", "AnalyzeClusterConfig"
)

proxy_config = FunctionTool(
    _proxy_config,
    description="Get specific proxy configuration for a single pod",
    name="proxy_config",
)

ProxyConfig, ProxyConfigConfig = create_typed_fn_tool(proxy_config, "kagent.tools.istio.ProxyConfig", "ProxyConfig")


# Function that runs the istioctl command in the shell
def _run_istioctl_command(command: str) -> str:
    return run_command("istioctl", command.split(" "))
