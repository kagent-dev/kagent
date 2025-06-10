from typing import Literal, Optional

from autogen_core.tools import FunctionTool
from typing_extensions import Annotated

from .._utils import create_typed_fn_tool
from ..common._shell import run_command

# Tools for running cilium-dbg command inside the Cilium pod


def _get_cilium_pod_name(
    node_name: Annotated[Optional[str], "The name of the node to get the Cilium pod name for"] = None,
) -> str:
    # Get the name of the Cilium pod in the cluster where we can run the cilium-dbg command
    if node_name:
        cilium_pod_name = run_command(
            "kubectl",
            [
                "get",
                "pod",
                "-l",
                "k8s-app=cilium",
                "-o",
                "name",
                "-n",
                "kube-system",
                "--field-selector",
                f"spec.nodeName={node_name}",
            ],
        )
    else:
        cilium_pod_name = run_command(
            "kubectl", ["get", "pod", "-l", "k8s-app=cilium", "-o", "name", "-n", "kube-system"]
        )
    if not cilium_pod_name:
        raise ValueError("No Cilium pod found in the cluster - make sure Cilium is installed and running")

    return cilium_pod_name.strip()


def _run_cilium_dbg_command(
    command: str, node_name: Annotated[Optional[str], "The name of the node to run the cilium-dbg command on"] = None
) -> str:
    cilium_pod_name = _get_cilium_pod_name(node_name)
    cmd_parts = command.split(" ")
    return run_command("kubectl", ["exec", "-it", cilium_pod_name, "-n", "kube-system", "--", "cilium-dbg", *cmd_parts])


def _get_endpoint_details(
    endpoint_id: Annotated[str, "The ID of the endpoint to get details for"],
    labels: Annotated[Optional[str], "The labels of the endpoint to get details for"] = None,
    output_format: Annotated[Literal["json", "yaml", "jsonpath"], "The output format of the endpoint details"] = "json",
    node_name: Annotated[Optional[str], "The name of the node to get the endpoint details for"] = None,
) -> str:
    if labels:
        return _run_cilium_dbg_command(f"endpoint get -l {labels} -o {output_format}", node_name)
    else:
        return _run_cilium_dbg_command(f"endpoint get {endpoint_id} -o {output_format}", node_name)


get_endpoint_details = FunctionTool(
    _get_endpoint_details, "List the details of an endpoint in the cluster", name="get_endpoint_details"
)

GetEndpointDetails, GetEndpointDetailsConfig = create_typed_fn_tool(
    get_endpoint_details, "kagent.tools.cilium.GetEndpointDetails", "GetEndpointDetails"
)


def _get_endpoint_logs(
    endpoint_id: Annotated[str, "The ID of the endpoint to get logs for"],
    node_name: Annotated[Optional[str], "The name of the node to get the endpoint logs for"] = None,
) -> str:
    return _run_cilium_dbg_command(f"endpoint get {endpoint_id}", node_name)


get_endpoint_logs = FunctionTool(
    _get_endpoint_logs, "Get the logs of an endpoint in the cluster", name="get_endpoint_logs"
)
GetEndpointLogs, GetEndpointLogsConfig = create_typed_fn_tool(
    get_endpoint_logs, "kagent.tools.cilium.GetEndpointLogs", "GetEndpointLogs"
)


def _get_endpoint_health(
    endpoint_id: Annotated[str, "The ID of the endpoint to get health for"],
    node_name: Annotated[Optional[str], "The name of the node to get the endpoint health for"] = None,
) -> str:
    return _run_cilium_dbg_command(f"endpoint get {endpoint_id}", node_name)


get_endpoint_health = FunctionTool(
    _get_endpoint_health, "Get the health of an endpoint in the cluster", name="get_endpoint_health"
)

GetEndpointHealth, GetEndpointHealthConfig = create_typed_fn_tool(
    get_endpoint_health, "kagent.tools.cilium.GetEndpointHealth", "GetEndpointHealth"
)


def _manage_endpoint_labels(
    endpoint_id: Annotated[str, "The ID of the endpoint to manage labels for"],
    labels: Annotated[dict[str, str], "The labels to manage for the endpoint"],
    action: Annotated[Literal["add", "delete"], "The action to perform on the labels"],
    node_name: Annotated[Optional[str], "The name of the node to manage the endpoint labels on"] = None,
) -> str:
    if action == "add":
        return _run_cilium_dbg_command(f"endpoint labels {endpoint_id} --add {labels}", node_name)
    elif action == "delete":
        return _run_cilium_dbg_command(f"endpoint labels {endpoint_id} --delete {labels}", node_name)


manage_endpoint_labels = FunctionTool(
    _manage_endpoint_labels,
    "Manage the labels (add or delete) of an endpoint in the cluster",
    name="manage_endpoint_labels",
)
ManageEndpointLabels, ManageEndpointLabelsConfig = create_typed_fn_tool(
    manage_endpoint_labels, "kagent.tools.cilium.ManageEndpointLabels", "ManageEndpointLabels"
)


def _manage_endpoint_configuration(
    endpoint_id: Annotated[str, "The ID of the endpoint to manage configuration for"],
    config: Annotated[
        list[str],
        "The configuration to manage for the endpoint provided as a list of key-value pairs (e.g. ['DropNotification=false', 'TraceNotification=false'])",
    ],
    node_name: Annotated[Optional[str], "The name of the node to manage the endpoint configuration on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"endpoint config {endpoint_id} {' '.join(config)}", node_name)


manage_endpoint_configuration = FunctionTool(
    _manage_endpoint_configuration,
    "Manage the configuration of an endpoint in the cluster",
    name="manage_endpoint_configuration",
)
ManageEndpointConfig, ManageEndpointConfigConfig = create_typed_fn_tool(
    manage_endpoint_configuration, "kagent.tools.cilium.ManageEndpointConfig", "ManageEndpointConfig"
)


def _disconnect_endpoint(
    endpoint_id: Annotated[str, "The ID of the endpoint to disconnect from the network"],
    node_name: Annotated[Optional[str], "The name of the node to disconnect the endpoint from"] = None,
) -> str:
    return _run_cilium_dbg_command(f"endpoint disconnect {endpoint_id}", node_name)


disconnect_endpoint = FunctionTool(
    _disconnect_endpoint, "Disconnect an endpoint from the network", name="disconnect_endpoint"
)
DisconnectEndpoint, DisconnectEndpointConfig = create_typed_fn_tool(
    disconnect_endpoint, "kagent.tools.cilium.DisconnectEndpoint", "DisconnectEndpoint"
)


def _get_endpoints_list(
    node_name: Annotated[Optional[str], "The name of the node to get the endpoints list for"] = None,
) -> str:
    return _run_cilium_dbg_command("endpoint list", node_name)


get_endpoints_list = FunctionTool(
    _get_endpoints_list,
    "Get the list of all endpoints in the cluster.",
    name="get_endpoints_list",
)

GetEndpointsList, GetEndpointsListConfig = create_typed_fn_tool(
    get_endpoints_list, "kagent.tools.cilium.GetEndpointsList", "GetEndpointsList"
)


def _list_identities(
    node_name: Annotated[Optional[str], "The name of the node to list the identities for"] = None,
) -> str:
    return _run_cilium_dbg_command("identity list", node_name)


list_identities = FunctionTool(_list_identities, "List all identities in the cluster", name="list_identities")
ListIdentities, ListIdentitiesConfig = create_typed_fn_tool(
    list_identities, "kagent.tools.cilium.ListIdentities", "ListIdentities"
)


def _get_identity_details(
    identity_id: Annotated[str, "The ID of the identity to get details for"],
    node_name: Annotated[Optional[str], "The name of the node to get the identity details for"] = None,
) -> str:
    return _run_cilium_dbg_command(f"identity get {identity_id}", node_name)


get_identity_details = FunctionTool(
    _get_identity_details, "Get the details of an identity in the cluster", name="get_identity_details"
)
GetIdentityDetails, GetIdentityDetailsConfig = create_typed_fn_tool(
    get_identity_details, "kagent.tools.cilium.GetIdentityDetails", "GetIdentityDetails"
)


def _show_configuration_options(
    list_all: Annotated[bool, "Whether to list all configuration options"] = False,
    list_read_only: Annotated[bool, "Whether to list read-only configuration options"] = False,
    list_options: Annotated[bool, "Whether to list options"] = False,
    node_name: Annotated[Optional[str], "The name of the node to show the configuration options for"] = None,
) -> str:
    if list_all:
        return _run_cilium_dbg_command("endpoint config --all", node_name)
    elif list_read_only:
        return _run_cilium_dbg_command("endpoint config -r", node_name)
    elif list_options:
        return _run_cilium_dbg_command("endpoint config --list-options", node_name)
    else:
        return _run_cilium_dbg_command("endpoint config", node_name)


show_configuration_options = FunctionTool(
    _show_configuration_options, "Show Cilium configuration options", name="show_configuration_options"
)
ShowConfigurationOptions, ShowConfigurationOptionsConfig = create_typed_fn_tool(
    show_configuration_options, "kagent.tools.cilium.ShowConfigurationOptions", "ShowConfigurationOptions"
)


def _toggle_configuration_option(
    option: Annotated[str, "The option to toggle"],
    value: Annotated[bool, "The value to set the option to"],
    node_name: Annotated[Optional[str], "The name of the node to toggle the configuration option on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"endpoint config {option}={'enable' if value else 'disable'}", node_name)


toggle_configuration_option = FunctionTool(
    _toggle_configuration_option, "Toggle a Cilium configuration option", name="toggle_configuration_option"
)
ToggleConfigurationOption, ToggleConfigurationOptionConfig = create_typed_fn_tool(
    toggle_configuration_option, "kagent.tools.cilium.ToggleConfigurationOption", "ToggleConfigurationOption"
)


def _request_debugging_information(
    node_name: Annotated[Optional[str], "The name of the node to request debugging information from"] = None,
) -> str:
    return _run_cilium_dbg_command("debuginfo", node_name)


request_debugging_information = FunctionTool(
    _request_debugging_information,
    "Request debugging information from Cilium agent",
    name="request_debugging_information",
)
RequestDebuggingInformation, RequestDebuggingInformationConfig = create_typed_fn_tool(
    request_debugging_information, "kagent.tools.cilium.RequestDebuggingInformation", "RequestDebuggingInformation"
)


def _display_encryption_state(
    node_name: Annotated[Optional[str], "The name of the node to display the encryption state for"] = None,
) -> str:
    return _run_cilium_dbg_command("encrypt status", node_name)


display_encryption_state = FunctionTool(
    _display_encryption_state, "Display the current encryption state", name="display_encryption_state"
)
DisplayEncryptionState, DisplayEncryptionStateConfig = create_typed_fn_tool(
    display_encryption_state, "kagent.tools.cilium.DisplayEncryptionState", "DisplayEncryptionState"
)


def _flush_ipsec_state(
    node_name: Annotated[Optional[str], "The name of the node to flush the IPsec state on"] = None,
) -> str:
    return _run_cilium_dbg_command("encrypt flush -f", node_name)


flush_ipsec_state = FunctionTool(_flush_ipsec_state, "Flush the IPsec state", name="flush_ipsec_state")
FlushIPsecState, FlushIPsecStateConfig = create_typed_fn_tool(
    flush_ipsec_state, "kagent.tools.cilium.FlushIPsecState", "FlushIPsecState"
)


def _list_envoy_config(
    resource_name: Annotated[
        Literal["certs", "clusters", "config", "listeners", "logging", "metrics", "serverinfo"],
        "The name of the Envoy config to list",
    ],
    node_name: Annotated[Optional[str], "The name of the node to list the Envoy config on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"envoy admin {resource_name}", node_name)


list_envoy_config = FunctionTool(_list_envoy_config, "List the Envoy configuration", name="list_envoy_config")
ListEnvoyConfig, ListEnvoyConfigConfig = create_typed_fn_tool(
    list_envoy_config, "kagent.tools.cilium.ListEnvoyConfig", "ListEnvoyConfig"
)


def _fqdn_cache(
    command: Annotated[Literal["list", "clean"], "The command to execute on the FQDN cache"],
    node_name: Annotated[Optional[str], "The name of the node to manage the FQDN cache on"] = None,
) -> str:
    if command == "clean":
        return _run_cilium_dbg_command("fqdn cache clean -f", node_name)
    else:
        return _run_cilium_dbg_command(f"fqdn cache {command}", node_name)


fqdn_cache = FunctionTool(_fqdn_cache, "Manage the FQDN cache", name="fqdn_cache")
FQDNCache, FQDNCacheConfig = create_typed_fn_tool(fqdn_cache, "kagent.tools.cilium.FQDNCache", "FQDNCache")


def _show_dns_names(
    node_name: Annotated[Optional[str], "The name of the node to show the DNS names for"] = None,
) -> str:
    return _run_cilium_dbg_command("dns names", node_name)


show_dns_names = FunctionTool(
    _show_dns_names, "Show the internal state Cilium has for DNS names/regexes", name="show_dns_names"
)
ShowDNSNames, ShowDNSNamesConfig = create_typed_fn_tool(
    show_dns_names, "kagent.tools.cilium.ShowDNSNames", "ShowDNSNames"
)


def _list_ip_addresses(
    node_name: Annotated[Optional[str], "The name of the node to list the IP addresses for"] = None,
) -> str:
    return _run_cilium_dbg_command("ip list", node_name)


list_ip_addresses = FunctionTool(
    _list_ip_addresses, "List the IP addresses in the userspace IPCache", name="list_ip_addresses"
)

ListIPAddresses, ListIPAddressesConfig = create_typed_fn_tool(
    list_ip_addresses, "kagent.tools.cilium.ListIPAddresses", "ListIPAddresses"
)


def _show_ip_cache_information(
    cidr: Annotated[str, "The CIDR to show information for"],
    labels: Annotated[Optional[str], "The identity labels"],
    node_name: Annotated[Optional[str], "The name of the node to show the IP cache information for"] = None,
) -> str:
    if labels:
        return _run_cilium_dbg_command(f"ip get --labels {labels}", node_name)
    else:
        return _run_cilium_dbg_command(f"ip get {cidr}", node_name)


show_ip_cache_information = FunctionTool(
    _show_ip_cache_information, "Show the information of the IP cache", name="show_ip_cache_information"
)
ShowIPCacheInformation, ShowIPCacheInformationConfig = create_typed_fn_tool(
    show_ip_cache_information, "kagent.tools.cilium.ShowIPCacheInformation", "ShowIPCacheInformation"
)


def _delete_key_from_kvstore(
    key: Annotated[str, "The key to delete from the kvstore"],
    node_name: Annotated[Optional[str], "The name of the node to delete the key from the kvstore on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"kvstore delete {key}", node_name)


delete_key_from_kvstore = FunctionTool(
    _delete_key_from_kvstore, "Delete a key from the kvstore", name="delete_key_from_kvstore"
)
DeleteKeyFromKVStore, DeleteKeyFromKVStoreConfig = create_typed_fn_tool(
    delete_key_from_kvstore, "kagent.tools.cilium.DeleteKeyFromKVStore", "DeleteKeyFromKVStore"
)


def _get_kvstore_key(
    key: Annotated[str, "The key to get from the kvstore"],
    node_name: Annotated[Optional[str], "The name of the node to get the key from the kvstore on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"kvstore get {key}", node_name)


get_kvstore_key = FunctionTool(_get_kvstore_key, "Get a key from the kvstore", name="get_kvstore_key")
GetKVStoreKey, GetKVStoreKeyConfig = create_typed_fn_tool(
    get_kvstore_key, "kagent.tools.cilium.GetKVStoreKey", "GetKVStoreKey"
)


def _set_kvstore_key(
    key: Annotated[str, "The key to set in the kvstore"],
    value: Annotated[str, "The value to set the key to"],
    node_name: Annotated[Optional[str], "The name of the node to set the key in the kvstore on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"kvstore set {key}={value}", node_name)


set_kvstore_key = FunctionTool(_set_kvstore_key, "Set a key in the kvstore", name="set_kvstore_key")
SetKVStoreKey, SetKVStoreKeyConfig = create_typed_fn_tool(
    set_kvstore_key, "kagent.tools.cilium.SetKVStoreKey", "SetKVStoreKey"
)


def _show_load_information(
    node_name: Annotated[Optional[str], "The name of the node to show the load information for"] = None,
) -> str:
    return _run_cilium_dbg_command("loadinfo", node_name)


show_load_information = FunctionTool(_show_load_information, "Show the load information", name="show_load_information")
ShowLoadInformation, ShowLoadInformationConfig = create_typed_fn_tool(
    show_load_information, "kagent.tools.cilium.ShowLoadInformation", "ShowLoadInformation"
)


def _list_local_redirect_policies(
    node_name: Annotated[Optional[str], "The name of the node to list the local redirect policies on"] = None,
) -> str:
    return _run_cilium_dbg_command("lrp list", node_name)


list_local_redirect_policies = FunctionTool(
    _list_local_redirect_policies, "List the local redirect policies", name="list_local_redirect_policies"
)
ListLocalRedirectPolicies, ListLocalRedirectPoliciesConfig = create_typed_fn_tool(
    list_local_redirect_policies, "kagent.tools.cilium.ListLocalRedirectPolicies", "ListLocalRedirectPolicies"
)


def _list_bpf_map_events(
    map_name: Annotated[str, "The name of the BPF map to show events for"],
    node_name: Annotated[Optional[str], "The name of the node to list the BPF map events on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"bpf map events {map_name}", node_name)


list_bpf_map_events = FunctionTool(_list_bpf_map_events, "List the events of the BPF maps", name="list_bpf_map_events")
ListBPFMapEvents, ListBPFMapEventsConfig = create_typed_fn_tool(
    list_bpf_map_events, "kagent.tools.cilium.ListBPFMapEvents", "ListBPFMapEvents"
)


def _get_bpf_map(
    map_name: Annotated[str, "The name of the BPF map to get"],
    node_name: Annotated[Optional[str], "The name of the node to get the BPF map on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"bpf map get {map_name}", node_name)


get_bpf_map = FunctionTool(_get_bpf_map, "Get the BPF map", name="get_bpf_map")
GetBPFMap, GetBPFMapConfig = create_typed_fn_tool(get_bpf_map, "kagent.tools.cilium.GetBPFMap", "GetBPFMap")


def _list_bpf_maps(node_name: Annotated[Optional[str], "The name of the node to list the BPF maps on"] = None) -> str:
    return _run_cilium_dbg_command("bpf map list", node_name)


list_bpf_maps = FunctionTool(_list_bpf_maps, "List all open BPF maps", name="list_bpf_maps")
ListBPFMaps, ListBPFMapsConfig = create_typed_fn_tool(list_bpf_maps, "kagent.tools.cilium.ListBPFMaps", "ListBPFMaps")


def _list_metrics(
    match_pattern: Annotated[Optional[str], "The pattern to match in the metrics"],
    node_name: Annotated[Optional[str], "The name of the node to list the metrics on"] = None,
) -> str:
    if match_pattern:
        return _run_cilium_dbg_command(f"metrics list --pattern {match_pattern}", node_name)
    else:
        return _run_cilium_dbg_command("metrics list", node_name)


list_metrics = FunctionTool(_list_metrics, "List the metrics", name="list_metrics")
ListMetrics, ListMetricsConfig = create_typed_fn_tool(list_metrics, "kagent.tools.cilium.ListMetrics", "ListMetrics")


def _list_cluster_nodes(
    node_name: Annotated[Optional[str], "The name of the node to list the cluster nodes on"] = None,
) -> str:
    return _run_cilium_dbg_command("nodes list", node_name)


list_cluster_nodes = FunctionTool(_list_cluster_nodes, "List the nodes in the cluster", name="list_cluster_nodes")
ListClusterNodes, ListClusterNodesConfig = create_typed_fn_tool(
    list_cluster_nodes, "kagent.tools.cilium.ListClusterNodes", "ListClusterNodes"
)


def _list_node_ids(node_name: Annotated[Optional[str], "The name of the node to list the node IDs on"] = None) -> str:
    return _run_cilium_dbg_command("nodeid list", node_name)


list_node_ids = FunctionTool(_list_node_ids, "List the node IDs and the associated IP addresses", name="list_node_ids")
ListNodeIds, ListNodeIdsConfig = create_typed_fn_tool(list_node_ids, "kagent.tools.cilium.ListNodeIds", "ListNodeIds")


def _display_policy_node_information(
    labels: Annotated[Optional[str], "The labels to display information for"],
    node_name: Annotated[Optional[str], "The name of the node to display the policy node information on"] = None,
) -> str:
    if labels:
        return _run_cilium_dbg_command(f"policy get {labels}", node_name)
    else:
        return _run_cilium_dbg_command("policy get", node_name)


display_policy_node_information = FunctionTool(
    _display_policy_node_information, "Display the policy node information", name="display_policy_node_information"
)
DisplayPolicyNodeInformation, DisplayPolicyNodeInformationConfig = create_typed_fn_tool(
    display_policy_node_information, "kagent.tools.cilium.DisplayPolicyNodeInformation", "DisplayPolicyNodeInformation"
)


def _delete_policy_rules(
    labels: Annotated[Optional[str], "The labels to delete the policy rules for"],
    all: Annotated[bool, "Whether to delete all policy rules"] = False,
    node_name: Annotated[Optional[str], "The name of the node to delete the policy rules on"] = None,
) -> str:
    if all:
        return _run_cilium_dbg_command("policy delete --all", node_name)
    else:
        return _run_cilium_dbg_command(f"policy delete {labels}", node_name)


delete_policy_rules = FunctionTool(_delete_policy_rules, "Delete the policy rules", name="delete_policy_rules")
DeletePolicyRules, DeletePolicyRulesConfig = create_typed_fn_tool(
    delete_policy_rules, "kagent.tools.cilium.DeletePolicyRules", "DeletePolicyRules"
)


def _display_selectors(
    node_name: Annotated[Optional[str], "The name of the node to display the selectors on"] = None,
) -> str:
    return _run_cilium_dbg_command("policy selectors", node_name)


display_selectors = FunctionTool(
    _display_selectors, "Display cached information about selectors", name="display_selectors"
)
DisplaySelectors, DisplaySelectorsConfig = create_typed_fn_tool(
    display_selectors, "kagent.tools.cilium.DisplaySelectors", "DisplaySelectors"
)


def _list_xdp_cidr_filters(
    node_name: Annotated[Optional[str], "The name of the node to list the XDP CIDR filters on"] = None,
) -> str:
    return _run_cilium_dbg_command("prefilter list", node_name)


list_xdp_cidr_filters = FunctionTool(
    _list_xdp_cidr_filters, "List the XDP CIDR filters (prefilter)", name="list_xdp_cidr_filters"
)
ListXDPCIDRFilters, ListXDPCIDRFiltersConfig = create_typed_fn_tool(
    list_xdp_cidr_filters, "kagent.tools.cilium.ListXDPCIDRFilters", "ListXDPCIDRFilters"
)


def _update_xdp_cidr_filters(
    cidr_prefixes: Annotated[list[str], "The list of CIDR prefixes to block"],
    revision: Annotated[Optional[int], "The update revision"],
    node_name: Annotated[Optional[str], "The name of the node to update the XDP CIDR filters on"] = None,
) -> str:
    return _run_cilium_dbg_command(
        f"prefilter update --cidr {' '.join(cidr_prefixes)} --revision {revision}", node_name
    )


update_xdp_cidr_filters = FunctionTool(
    _update_xdp_cidr_filters, "Update the XDP CIDR filters", name="update_xdp_cidr_filters"
)
UpdateXDPCIDRFilters, UpdateXDPCIDRFiltersConfig = create_typed_fn_tool(
    update_xdp_cidr_filters, "kagent.tools.cilium.UpdateXDPCIDRFilters", "UpdateXDPCIDRFilters"
)


def _delete_xdp_cidr_filters(
    cidr_prefixes: Annotated[list[str], "The list of CIDR prefixes to delete   "],
    revision: Annotated[Optional[int], "The update revision"],
    node_name: Annotated[Optional[str], "The name of the node to delete the XDP CIDR filters on"] = None,
) -> str:
    return _run_cilium_dbg_command(
        f"prefilter delete --cidr {' '.join(cidr_prefixes)} --revision {revision}", node_name
    )


delete_xdp_cidr_filters = FunctionTool(
    _delete_xdp_cidr_filters, "Delete the XDP CIDR filters", name="delete_xdp_cidr_filters"
)
DeleteXDPCIDRFilters, DeleteXDPCIDRFiltersConfig = create_typed_fn_tool(
    delete_xdp_cidr_filters, "kagent.tools.cilium.DeleteXDPCIDRFilters", "DeleteXDPCIDRFilters"
)


def _validate_cilium_network_policies(
    enable_k8s: Annotated[bool, "Enable the k8s clientset"] = True,
    enable_k8s_api_discovery: Annotated[
        bool, "Enable discovery of Kubernetes API groups and resources with the discovery API"
    ] = True,
    node_name: Annotated[Optional[str], "The name of the node to validate the Cilium network policies on"] = None,
) -> str:
    return _run_cilium_dbg_command(
        f"preflight validate-cnp {'--enable-k8s' if enable_k8s else ''} {'--enable-k8s-api-discovery' if enable_k8s_api_discovery else ''}",
        node_name,
    )


validate_cilium_network_policies = FunctionTool(
    _validate_cilium_network_policies,
    "Validate the Cilium network policies. It's recommended to run this before upgrading Cilium to ensure all policies are valid.",
    name="validate_cilium_network_policies",
)
ValidateCiliumNetworkPolicies, ValidateCiliumNetworkPoliciesConfig = create_typed_fn_tool(
    validate_cilium_network_policies,
    "kagent.tools.cilium.ValidateCiliumNetworkPolicies",
    "ValidateCiliumNetworkPolicies",
)


def _list_pcap_recorders(
    node_name: Annotated[Optional[str], "The name of the node to list the pcap recorders on"] = None,
) -> str:
    return _run_cilium_dbg_command("recorder list", node_name)


list_pcap_recorders = FunctionTool(_list_pcap_recorders, "List the pcap recorders", name="list_pcap_recorders")
ListPCAPRecorders, ListPCAPRecordersConfig = create_typed_fn_tool(
    list_pcap_recorders, "kagent.tools.cilium.ListPCAPRecorders", "ListPCAPRecorders"
)


def _get_pcap_recorder(
    recorder_id: Annotated[str, "The ID of the pcap recorder to get"],
    node_name: Annotated[Optional[str], "The name of the node to get the pcap recorder on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"recorder get {recorder_id}", node_name)


get_pcap_recorder = FunctionTool(_get_pcap_recorder, "Displays the individual pcap recorder", name="get_pcap_recorder")
GetPCAPRecorder, GetPCAPRecorderConfig = create_typed_fn_tool(
    get_pcap_recorder, "kagent.tools.cilium.GetPCAPRecorder", "GetPCAPRecorder"
)


def _delete_pcap_recorder(
    recorder_id: Annotated[str, "The ID of the pcap recorder to delete"],
    node_name: Annotated[Optional[str], "The name of the node to delete the pcap recorder on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"recorder delete {recorder_id}", node_name)


delete_pcap_recorder = FunctionTool(_delete_pcap_recorder, "Delete the pcap recorder", name="delete_pcap_recorder")
DeletePCAPRecorder, DeletePCAPRecorderConfig = create_typed_fn_tool(
    delete_pcap_recorder, "kagent.tools.cilium.DeletePCAPRecorder", "DeletePCAPRecorder"
)


def _update_pcap_recorder(
    recorder_id: Annotated[str, "The ID of the pcap recorder to update"],
    filters: Annotated[list[str], "List of filters ('<srcCIDR> <srcPort> <dstCIDR> <dstPort> <proto>')"],
    caplen: Annotated[int, "Capture length (0 is full capture)"],
    id: Annotated[int, "Identifier for the recorder"] = 0,
    node_name: Annotated[Optional[str], "The name of the node to update the pcap recorder on"] = None,
) -> str:
    return _run_cilium_dbg_command(
        f"recorder update {recorder_id} --filters {' '.join(filters)} --caplen {caplen} --id {id}", node_name
    )


update_pcap_recorder = FunctionTool(_update_pcap_recorder, "Update the pcap recorder", name="update_pcap_recorder")
UpdatePCAPRecorder, UpdatePCAPRecorderConfig = create_typed_fn_tool(
    update_pcap_recorder, "kagent.tools.cilium.UpdatePCAPRecorder", "UpdatePCAPRecorder"
)


def _list_services(
    show_cluster_mesh_affinity: Annotated[bool, "Show clustermesh affinity if available"] = False,
    node_name: Annotated[Optional[str], "The name of the node to list the services on"] = None,
) -> str:
    return _run_cilium_dbg_command(
        f"service list {'--clustermesh-affinity' if show_cluster_mesh_affinity else ''}", node_name
    )


list_services = FunctionTool(_list_services, "List the services", name="list_services")
ListServices, ListServicesConfig = create_typed_fn_tool(
    list_services, "kagent.tools.cilium.ListServices", "ListServices"
)


def _get_service_information(
    service_id: Annotated[str, "The ID of the service to get information for"],
    node_name: Annotated[Optional[str], "The name of the node to get the service information on"] = None,
) -> str:
    return _run_cilium_dbg_command(f"service get {service_id}", node_name)


get_service_information = FunctionTool(
    _get_service_information, "Get the information of the service", name="get_service_information"
)
GetServiceInformation, GetServiceInformationConfig = create_typed_fn_tool(
    get_service_information, "kagent.tools.cilium.GetServiceInformation", "GetServiceInformation"
)


def _delete_service(
    service_id: Annotated[str, "The ID of the service to delete"],
    all: Annotated[bool, "Whether to delete all services"] = False,
    node_name: Annotated[Optional[str], "The name of the node to delete the service on"] = None,
) -> str:
    if all:
        return _run_cilium_dbg_command("service delete --all", node_name)
    else:
        return _run_cilium_dbg_command(f"service delete {service_id}", node_name)


delete_service = FunctionTool(_delete_service, "Delete the service", name="delete_service")
DeleteService, DeleteServiceConfig = create_typed_fn_tool(
    delete_service, "kagent.tools.cilium.DeleteService", "DeleteService"
)


def _update_service(
    backend_weights: Annotated[list[int], "The backend weights to update the service with"],
    backends: Annotated[list[str], "The backend address or addresses (<IP:Port>) "],
    frontend: Annotated[str, "The frontend address or addresses (<IP:Port>) "],
    id: Annotated[int, "Identifier"],
    k8s_cluster_internal: Annotated[
        bool, "Set the service as cluster-internal for externalTrafficPolicy=Local xor internalTrafficPolicy=Local"
    ],
    k8s_ext_traffic_policy: Annotated[
        Literal["Local", "Cluster"], "Set the externalTrafficPolicy for Kubernetes service"
    ] = "Cluster",
    k8s_external: Annotated[bool, "Set the service as Kubernetes ExternalIPs"] = False,
    k8s_host_port: Annotated[bool, "Set the service as Kubernetes HostPort"] = False,
    k8s_int_traffic_policy: Annotated[
        Literal["Local", "Cluster"], "Set the service with Kubernetes internalTrafficPolicy as Local or Cluster"
    ] = "Cluster",
    k8s_load_balancer: Annotated[bool, "Set the service as Kubernetes LoadBalancer"] = False,
    k8s_node_port: Annotated[bool, "Set the service as Kubernetes NodePort"] = False,
    local_redirect: Annotated[bool, "Set the service as local redirect"] = False,
    protocol: Annotated[Literal["TCP", "UDP"], "Set the service protocol"] = "TCP",
    states: Annotated[Literal["active", "terminating", "quarantined", "maintenance"], "Backend states"] = "active",
    node_name: Annotated[Optional[str], "The name of the node to update the service on"] = None,
) -> str:
    return _run_cilium_dbg_command(
        f"service update {id} --backends {' '.join(backends)} {'--backend-weights ' + ' '.join(backend_weights) if backend_weights else ''} --frontend {frontend} {'--k8s-cluster-internal' if k8s_cluster_internal else ''} {'--k8s-ext-traffic-policy ' + k8s_ext_traffic_policy if k8s_ext_traffic_policy else ''} {'--k8s-external' if k8s_external else ''} {'--k8s-host-port' if k8s_host_port else ''} {'--k8s-int-traffic-policy ' + k8s_int_traffic_policy if k8s_int_traffic_policy else ''} {'--k8s-load-balancer' if k8s_load_balancer else ''} {'--k8s-node-port' if k8s_node_port else ''} {'--local-redirect' if local_redirect else ''} --protocol {protocol} --states {states}",
        node_name,
    )


update_service = FunctionTool(_update_service, "Update the service", name="update_service")
UpdateService, UpdateServiceConfig = create_typed_fn_tool(
    update_service, "kagent.tools.cilium.UpdateService", "UpdateService"
)


def _get_daemon_status(
    show_all_addresses: Annotated[bool, "Show all addresses, not just count"] = False,
    show_all_clusters: Annotated[bool, "Show all clusters"] = False,
    show_all_controllers: Annotated[bool, "Show all controllers, not just failing"] = False,
    show_health: Annotated[bool, "Show health status, not just failing"] = False,
    show_all_nodes: Annotated[bool, "Show all nodes, not just localhost"] = False,
    show_all_redirects: Annotated[bool, "Show all redirects"] = False,
    brief: Annotated[bool, "Show one-line status message"] = False,
    node_name: Annotated[Optional[str], "The name of the node to get the daemon status on"] = None,
) -> str:
    return _run_cilium_dbg_command(
        f"status {'--all-addresses' if show_all_addresses else ''} {'--all-clusters' if show_all_clusters else ''} {'--all-controllers' if show_all_controllers else ''} {'--health' if show_health else ''} {'--all-nodes' if show_all_nodes else ''} {'--all-redirects' if show_all_redirects else ''} {'--brief' if brief else ''}",
        node_name,
    )


get_daemon_status = FunctionTool(_get_daemon_status, "Get the status of the daemon", name="get_daemon_status")
GetDaemonStatus, GetDaemonStatusConfig = create_typed_fn_tool(
    get_daemon_status, "kagent.tools.cilium.GetDaemonStatus", "GetDaemonStatus"
)
