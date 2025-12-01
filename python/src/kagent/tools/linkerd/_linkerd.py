from typing import Annotated, Optional

from autogen_core.tools import FunctionTool

from .._utils import create_typed_fn_tool
from ..common import run_command


def _run_linkerd_command(command: str, timeout: Optional[float] = None) -> str:
    """Run a linkerd command with simple arguments."""
    return run_command("linkerd", command.split(), timeout=timeout)


def _run_linkerd_shell_command(command: str, timeout: Optional[float] = None) -> str:
    """Run a linkerd command that requires shell features (e.g., pipes)."""
    return run_command("sh", ["-c", command], timeout=timeout)


async def _version() -> str:
    return _run_linkerd_command("version")


async def _install_linkerd(
    ha: Annotated[bool, "Install the high-availability configuration"] = False,
    skip_crds: Annotated[bool, "Skip applying CRDs if they already exist"] = False,
) -> str:
    flags = " --ha" if ha else ""
    outputs: list[str] = []

    if not skip_crds:
        outputs.append(_run_linkerd_shell_command(f"linkerd install --crds{flags} | kubectl apply -f -"))

    outputs.append(_run_linkerd_shell_command(f"linkerd install{flags} | kubectl apply -f -"))
    return "\n".join(outputs)


async def _check(pre: Annotated[bool, "Run pre-installation checks"] = False) -> str:
    return _run_linkerd_command(f"check{' --pre' if pre else ''}")


async def _proxy_check(
    ns: Annotated[Optional[str], "Namespace to validate proxy health for"] = None,
) -> str:
    return _run_linkerd_command(f"check --proxy{' -n ' + ns if ns else ''}")


async def _viz_install(
    ha: Annotated[bool, "Install the viz extension in high-availability mode"] = False,
) -> str:
    flags = " --ha" if ha else ""
    return _run_linkerd_shell_command(f"linkerd viz install{flags} | kubectl apply -f -")


async def _viz_check() -> str:
    return _run_linkerd_command("viz check")


async def _edges(
    resource: Annotated[str, "Target resource to inspect (e.g., deploy/nginx)"],
    ns: Annotated[Optional[str], "Namespace of the resource"] = None,
) -> str:
    return _run_linkerd_command(f"edges {resource}{' -n ' + ns if ns else ''}")


async def _stat(
    resource: Annotated[str, "Target resource to inspect (e.g., deploy/nginx)"],
    ns: Annotated[Optional[str], "Namespace of the resource"] = None,
    time_window: Annotated[Optional[str], "Time window (e.g., 1m, 5m)"] = "1m",
) -> str:
    return _run_linkerd_command(
        f"stat {resource}{' -n ' + ns if ns else ''}{' -t ' + time_window if time_window else ''}"
    )


async def _routes(
    resource: Annotated[str, "Target resource to inspect routes for (e.g., deploy/nginx)"],
    ns: Annotated[Optional[str], "Namespace of the resource"] = None,
) -> str:
    return _run_linkerd_command(f"routes {resource}{' -n ' + ns if ns else ''}")


async def _controller_metrics() -> str:
    return _run_linkerd_command("diagnostics controller-metrics")


async def _diagnostics_endpoints(
    authority: Annotated[str, "Authority to introspect (e.g., web.linkerd-viz.svc.cluster.local:8084)"],
) -> str:
    return _run_linkerd_command(f"diagnostics endpoints {authority}")


async def _diagnostics_policy(
    resource: Annotated[str, "Resource to inspect policy state for (e.g., deploy/web)"],
    port: Annotated[str, "Port to query on the resource (e.g., 80)"],
    ns: Annotated[Optional[str], "Namespace of the resource"] = None,
) -> str:
    ns_flag = f" -n {ns}" if ns else ""
    return _run_linkerd_command(f"diagnostics policy{ns_flag} {resource} {port}")


async def _diagnostics_profile(
    resource: Annotated[str, "Resource to inspect profile/service discovery state for (e.g., deploy/web)"],
    ns: Annotated[Optional[str], "Namespace of the resource"] = None,
) -> str:
    return _run_linkerd_command(f"diagnostics profile {resource}{' -n ' + ns if ns else ''}")


async def _tap(
    resource: Annotated[str, "Target resource to tap (e.g., ns/simple-app or deploy/nginx)"],
    ns: Annotated[Optional[str], "Namespace of the resource"] = None,
    duration: Annotated[
        Optional[str],
        "Not executed here; included for compatibility. The suggested command streams until stopped.",
    ] = "10s",
    max_rps: Annotated[Optional[int], "Optional max sample rate to include in the suggested command"] = None,
) -> str:
    target_display = resource if not ns else f"{resource} (namespace: {ns})"
    example_command = f"linkerd viz tap {resource}{' -n ' + ns if ns else ''}{' --max-rps ' + str(max_rps) if max_rps else ''}"

    return (
        'The "linkerd tap" command you are trying to use is deprecated and expects usage via the "linkerd viz tap" command instead.\n\n'
        'Unfortunately, I cannot directly run the "linkerd viz tap" command in this environment. But I can help you craft the exact command you should run on your local machine or environment with the Linkerd CLI installed.\n\n'
        f'For example, to tap the target "{target_display}" you want to observe, the command is:\n\n'
        f"{example_command}\n\n"
        "This command will stream live requests until you stop it (Ctrl+C). You can add filters such as --method, --to, or --path to narrow down the requests.\n\n"
        "Would you like me to help you create specific tap commands or assist with any other Linkerd diagnostics?"
    )


async def _proxy_metrics(
    pod: Annotated[str, "Pod name to retrieve proxy metrics for"],
    ns: Annotated[Optional[str], "Namespace of the pod"] = None,
) -> str:
    return _run_linkerd_command(f"diagnostics proxy-metrics {pod}{' -n ' + ns if ns else ''}")


version = FunctionTool(
    _version,
    description="Returns the Linkerd CLI client version and control-plane version information.",
    name="version",
)

Version, VersionConfig = create_typed_fn_tool(version, "kagent.tools.linkerd.Version", "Version")

install_linkerd = FunctionTool(
    _install_linkerd,
    description="Install or upgrade the Linkerd control plane (CRDs plus core components).",
    name="install_linkerd",
)

InstallLinkerd, InstallLinkerdConfig = create_typed_fn_tool(
    install_linkerd, "kagent.tools.linkerd.InstallLinkerd", "InstallLinkerd"
)

check = FunctionTool(
    _check,
    description="Run Linkerd pre- or post-installation checks.",
    name="check",
)

Check, CheckConfig = create_typed_fn_tool(check, "kagent.tools.linkerd.Check", "Check")

proxy_check = FunctionTool(
    _proxy_check,
    description="Validate Linkerd proxies in a namespace.",
    name="proxy_check",
)

ProxyCheck, ProxyCheckConfig = create_typed_fn_tool(proxy_check, "kagent.tools.linkerd.ProxyCheck", "ProxyCheck")

viz_install = FunctionTool(
    _viz_install,
    description="Install the Linkerd viz extension.",
    name="viz_install",
)

VizInstall, VizInstallConfig = create_typed_fn_tool(viz_install, "kagent.tools.linkerd.VizInstall", "VizInstall")

viz_check = FunctionTool(
    _viz_check,
    description="Validate the Linkerd viz extension.",
    name="viz_check",
)

VizCheck, VizCheckConfig = create_typed_fn_tool(viz_check, "kagent.tools.linkerd.VizCheck", "VizCheck")

edges = FunctionTool(
    _edges,
    description="Inspect live connections between services.",
    name="edges",
)

Edges, EdgesConfig = create_typed_fn_tool(edges, "kagent.tools.linkerd.Edges", "Edges")

stat = FunctionTool(
    _stat,
    description="Show success rates, latencies, and request volumes for a resource.",
    name="stat",
)

Stat, StatConfig = create_typed_fn_tool(stat, "kagent.tools.linkerd.Stat", "Stat")

routes = FunctionTool(
    _routes,
    description="Display per-route success rates for a resource.",
    name="routes",
)

Routes, RoutesConfig = create_typed_fn_tool(routes, "kagent.tools.linkerd.Routes", "Routes")

tap = FunctionTool(
    _tap,
    description="Provide instructions for running linkerd viz tap manually (linkerd tap is deprecated and not executed here).",
    name="tap",
)

Tap, TapConfig = create_typed_fn_tool(tap, "kagent.tools.linkerd.Tap", "Tap")

controller_metrics = FunctionTool(
    _controller_metrics,
    description="Fetch metrics directly from Linkerd control-plane containers.",
    name="controller_metrics",
)

ControllerMetrics, ControllerMetricsConfig = create_typed_fn_tool(
    controller_metrics, "kagent.tools.linkerd.ControllerMetrics", "ControllerMetrics"
)

diagnostics_endpoints = FunctionTool(
    _diagnostics_endpoints,
    description="Introspect Linkerd's service discovery state for an authority.",
    name="diagnostics_endpoints",
)

DiagnosticsEndpoints, DiagnosticsEndpointsConfig = create_typed_fn_tool(
    diagnostics_endpoints, "kagent.tools.linkerd.DiagnosticsEndpoints", "DiagnosticsEndpoints"
)

diagnostics_policy = FunctionTool(
    _diagnostics_policy,
    description="Inspect Linkerd policy state for a resource.",
    name="diagnostics_policy",
)

DiagnosticsPolicy, DiagnosticsPolicyConfig = create_typed_fn_tool(
    diagnostics_policy, "kagent.tools.linkerd.DiagnosticsPolicy", "DiagnosticsPolicy"
)

diagnostics_profile = FunctionTool(
    _diagnostics_profile,
    description="Inspect Linkerd profile/service discovery state for a resource.",
    name="diagnostics_profile",
)

DiagnosticsProfile, DiagnosticsProfileConfig = create_typed_fn_tool(
    diagnostics_profile, "kagent.tools.linkerd.DiagnosticsProfile", "DiagnosticsProfile"
)

proxy_metrics = FunctionTool(
    _proxy_metrics,
    description="Fetch metrics from a specific Linkerd proxy.",
    name="proxy_metrics",
)

ProxyMetrics, ProxyMetricsConfig = create_typed_fn_tool(
    proxy_metrics, "kagent.tools.linkerd.ProxyMetrics", "ProxyMetrics"
)
