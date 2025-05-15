from typing import Literal

from autogen_core.tools import FunctionTool
from typing_extensions import Annotated

from .._utils import create_typed_fn_tool
from ..common._shell import run_command


def _cilium_status_and_version() -> str:
    status = _run_cilium_cli("status")
    version = _run_cilium_cli("version")
    return f"{status}\n{version}"


cilium_status_and_version = FunctionTool(
    _cilium_status_and_version,
    "Get the status and version of Cilium installation.",
    name="cilium_status_and_version",
)

CiliumStatusAndVersion, CiliumStatusAndVersionConfig = create_typed_fn_tool(
    cilium_status_and_version, "kagent.tools.cilium.CiliumStatusAndVersion", "CiliumStatusAndVersion"
)


def _upgrade_cilium(
    cluster_name: Annotated[str, "The name of the cluster to upgrade Cilium on"] = None,
    datapath_mode: Annotated[
        Literal["tunnel", "native", "aws-eni", "gke", "azure", "aks-byocni"], "The datapath mode to use for Cilium"
    ] = None,
) -> str:
    return _run_cilium_cli(
        f"upgrade f{'' if cluster_name else '--cluster-name {cluster_name}'} f{'' if datapath_mode else '--datapath-mode {datapath_mode}'}"
    )


upgrade_cilium = FunctionTool(
    _upgrade_cilium,
    "Upgrade Cilium on the cluster.",
    name="upgrade_cilium",
)

UpgradeCilium, UpgradeCiliumConfig = create_typed_fn_tool(
    upgrade_cilium, "kagent.tools.cilium.UpgradeCilium", "UpgradeCilium"
)


def _install_cilium(
    cluster_name: Annotated[str, "The name of the cluster to install Cilium on"] = None,
    cluster_id: Annotated[str, "The ID of the cluster to install Cilium on"] = None,
    datapath_mode: Annotated[
        Literal["tunnel", "native", "aws-eni", "gke", "azure", "aks-byocni"], "The datapath mode to use for Cilium"
    ] = None,
) -> str:
    return _run_cilium_cli(
        f"install f{'' if cluster_name else '--set cluster.name={cluster_name}'} f{'' if cluster_id else '--set cluster.id={cluster_id}'} f{'' if datapath_mode else '--datapath-mode {datapath_mode}'}"
    )


install_cilium = FunctionTool(
    _install_cilium,
    "Install Cilium on the cluster.",
    name="install_cilium",
)

InstallCilium, InstallCiliumConfig = create_typed_fn_tool(
    install_cilium, "kagent.tools.cilium.InstallCilium", "InstallCilium"
)


def _uninstall_cilium() -> str:
    return _run_cilium_cli("uninstall")


uninstall_cilium = FunctionTool(
    _uninstall_cilium,
    "Uninstall Cilium from the cluster.",
    name="uninstall_cilium",
)

UninstallCilium, UninstallCiliumConfig = create_typed_fn_tool(
    uninstall_cilium, "kagent.tools.cilium.UninstallCilium", "UninstallCilium"
)


def _list_bgp_peers(
    agent_pod_selector: Annotated[str, "Label on cilium-agent pods to select with"] = "k8s-app=cilium",
    node: Annotated[str, "Node from which BGP status will be fetched, omit to select all nodes"] = None,
) -> str:
    return _run_cilium_cli(
        f"bgp peers f{'' if agent_pod_selector else '--agent-pod-selector {agent_pod_selector}'} f{'' if node else '--node {node}'}"
    )


list_bgp_peers = FunctionTool(
    _list_bgp_peers,
    "Lists BGP peering state",
    name="list_bgp_peers",
)
ListBGPPeers, ListBGPPeersConfig = create_typed_fn_tool(
    list_bgp_peers, "kagent.tools.cilium.ListBGPPeers", "ListBGPPeers"
)


def _list_bgp_routes(
    route_type: Annotated[Literal["available", "advertised"], "Type of routes to list"] = "available",
    address_family: Annotated[Literal["ipv4", "ipv6"], "Address family"] = "ipv4",
    safi: Annotated[Literal["unicast", "multicast"], "Subsequent address family"] = "unicast",
    vrouter_asn: Annotated[int, "ASN of the vRouter"] = None,
    peer_address: Annotated[str, "IP address of the peer"] = None,
    agent_pod_selector: Annotated[str, "Label on cilium-agent pods to select with"] = "k8s-app=cilium",
) -> str:
    return _run_cilium_cli(
        f"bgp routes {route_type} {address_family} {safi} f{'' if vrouter_asn else vrouter_asn} f{'' if peer_address else peer_address} f{'' if agent_pod_selector else '--agent-pod-selector {agent_pod_selector}'}"
    )


list_bgp_routes = FunctionTool(
    _list_bgp_routes,
    "Lists BGP routes",
    name="list_bgp_routes",
)

ListBGPRoutes, ListBGPRoutesConfig = create_typed_fn_tool(
    list_bgp_routes, "kagent.tools.cilium.ListBGPRoutes", "ListBGPRoutes"
)


def _connect_to_remote_cluster(
    parallel: Annotated[int, "Number of parallel connection of destination cluster"] = 1,
    source_endpoint: Annotated[list[str], "IP of ClusterMesh service of source cluster"] = None,
    destination_context: Annotated[
        list[str], "Comma separated list of Kubernetes configuration contexts of destination cluster"
    ] = None,
    connection_mode: Annotated[Literal["unicast", "bidirectional", "mesh"], "Connection mode"] = "bidirectional",
    destination_endpoint: Annotated[list[str], "IP of ClusterMesh service of destination cluster"] = None,
) -> str:
    return _run_cilium_cli(
        f"clustermesh connect {connection_mode} f{'' if destination_context else '--destination-context {destination_context}'} f{'' if destination_endpoint else '--destination-endpoint {destination_endpoint}'} f{'' if parallel else '--parallel {parallel}'} f{'' if source_endpoint else '--source-endpoint {source_endpoint}'}"
    )


connect_to_remote_cluster = FunctionTool(
    _connect_to_remote_cluster,
    "Connect to a remote cluster (clustermesh)",
    name="connect_to_remote_cluster",
)
ConnectToRemoteCluster, ConnectToRemoteClusterConfig = create_typed_fn_tool(
    connect_to_remote_cluster, "kagent.tools.cilium.ConnectToRemoteCluster", "ConnectToRemoteCluster"
)


def _disconnect_remote_cluster(
    connection_mode: Annotated[Literal["unicast", "bidirectional", "mesh"], "Connection mode"] = "bidirectional",
    destination_context: Annotated[
        list[str], "Comma separated list of Kubernetes configuration contexts of destination cluster"
    ] = None,
) -> str:
    return _run_cilium_cli(
        f"clustermesh disconnect {connection_mode} f{'' if destination_context else '--destination-context {destination_context}'}"
    )


disconnect_remote_cluster = FunctionTool(
    _disconnect_remote_cluster,
    "Disconnect from a remote cluster (clustermesh)",
    name="disconnect_remote_cluster",
)
DisconnectRemoteCluster, DisconnectRemoteClusterConfig = create_typed_fn_tool(
    disconnect_remote_cluster, "kagent.tools.cilium.DisconnectRemoteCluster", "DisconnectRemoteCluster"
)


def _toggle_cluster_mesh(
    enable_mesh: Annotated[bool, "Enable or disable clustermesh"] = True,
    enable_kvstoremesh: Annotated[bool, "Enable Kvstore mesh"] = False,
) -> str:
    return _run_cilium_cli(
        f"clustermesh {'enable' if enable_mesh else 'disable'} f{'' if enable_kvstoremesh else '--enable-kvstoremesh {enable_kvstoremesh}'}"
    )


toggle_cluster_mesh = FunctionTool(
    _toggle_cluster_mesh,
    "Enable or disable clustermesh ability in a cluster using Helm",
    name="toggle_cluster_mesh",
)

ToggleClusterMesh, ToggleClusterMeshConfig = create_typed_fn_tool(
    toggle_cluster_mesh, "kagent.tools.cilium.ToggleClusterMesh", "ToggleClusterMesh"
)


def _show_cluster_mesh_status() -> str:
    return _run_cilium_cli("clustermesh status")


show_cluster_mesh_status = FunctionTool(
    _show_cluster_mesh_status, "Show clustermesh status", name="show_cluster_mesh_status"
)

ShowClusterMeshStatus, ShowClusterMeshStatusConfig = create_typed_fn_tool(
    show_cluster_mesh_status, "kagent.tools.cilium.ShowClusterMeshStatus", "ShowClusterMeshStatus"
)


def _show_features_status() -> str:
    return _run_cilium_cli("features status")


show_features_status = FunctionTool(_show_features_status, "Show feature status", name="show_features_status")

ShowFeaturesStatus, ShowFeaturesStatusConfig = create_typed_fn_tool(
    show_features_status, "kagent.tools.cilium.ShowFeaturesStatus", "ShowFeaturesStatus"
)


def _toggle_hubble(enable: Annotated[bool, "Enable or disable Hubble"] = True) -> str:
    return _run_cilium_cli(f"hubble {'enable' if enable else 'disable'}")


toggle_hubble = FunctionTool(_toggle_hubble, "Toggle Hubble", name="toggle_hubble")

ToggleHubble, ToggleHubbleConfig = create_typed_fn_tool(
    toggle_hubble, "kagent.tools.cilium.ToggleHubble", "ToggleHubble"
)


def _run_cilium_cli(command: str) -> str:
    cmd_parts = command.split(" ")
    cmd_parts = [part for part in cmd_parts if part]  # Remove empty strings from the list
    return run_command("cilium", cmd_parts)
