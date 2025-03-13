import os
import platform
import re
import tempfile
from dataclasses import dataclass
from typing import Annotated, Optional

from autogen_core.tools import FunctionTool

from .._utils import create_typed_fn_tool
from ..common import run_command


@dataclass
class GatewayPluginStatus:
    installed: bool
    version: Optional[str] = None
    architecture: Optional[str] = None
    download_time: Optional[float] = None
    error_message: Optional[str] = None


def _verify_gateway_plugin(
    version: Annotated[Optional[str], "Version of the Gateway API plugin to verify. If None, checks latest"] = None,
    namespace: Annotated[str, "Namespace where Argo Rollouts is installed"] = "argo-rollouts",
    should_install: Annotated[bool, "Flag to determine if the plugin should be installed if not present"] = True,
) -> GatewayPluginStatus:
    """
    Verify Gateway API plugin installation for Argo Rollouts.
    Checks ConfigMap and logs for proper installation and configuration.
    """
    # First check if the ConfigMap exists and is properly configured
    cmd = ["get", "configmap", "argo-rollouts-config", "-n", namespace, "-o", "yaml"]
    try:
        config_map = run_command(command="kubectl", args=cmd)
        if "argoproj-labs/gatewayAPI" not in config_map:
            if should_install:
                return _configure_gateway_plugin(version, namespace)
            else:
                return GatewayPluginStatus(
                    installed=False, error_message="Gateway API plugin is not configured and installation is disabled"
                )
        return GatewayPluginStatus(installed=True, error_message="Gateway API plugin is already configured")
    except Exception as e:
        if should_install:
            return _configure_gateway_plugin(version, namespace)
        else:
            return GatewayPluginStatus(installed=False, error_message=f"Error verifying plugin: {str(e)}")


def _configure_gateway_plugin(
    version: Optional[str],
    namespace: str,
) -> GatewayPluginStatus:
    """
    Configure the Gateway API plugin by creating or updating the ConfigMap.
    """
    try:
        # Determine system architecture
        arch = _get_system_architecture()

        # If version not specified, get latest from GitHub
        if not version:
            version = _get_latest_version()

        # Create ConfigMap manifest
        config_map = f"""
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: {namespace}
data:
  trafficRouterPlugins: |-
    - name: "argoproj-labs/gatewayAPI"
      location: "https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/releases/download/v{version}/gatewayapi-plugin-{arch}"
"""

        # Create a temp file with the ConfigMap that adds the trafficRouterPlugins for the Kubernetes Gateway API
        with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as temp_file:
            temp_file.write(config_map)
            temp_file.flush()

            try:
                # Apply the ConfigMap using the temporary file
                cmd = ["apply", "-f", temp_file.name]
                run_command(command="kubectl", args=cmd)
                os.unlink(temp_file.name)
                return GatewayPluginStatus(
                    installed=True,
                    version=version,
                    architecture=arch,
                )
            except Exception as e:
                os.unlink(temp_file.name)
                return GatewayPluginStatus(
                    installed=False, error_message=f"Failed to configure Gateway API plugin: {str(e)}"
                )
    except Exception as e:
        return GatewayPluginStatus(installed=False, error_message=f"Error during plugin configuration: {str(e)}")


def _get_system_architecture() -> str:
    """
    Determine the system architecture for plugin download.
    """
    system = platform.system().lower()
    machine = platform.machine().lower()

    # Map machine architecture to supported plugin architecture
    # See https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/releases/ for supported architectures
    arch_map = {
        "x86_64": "amd64",
        "aarch64": "arm64",
        "armv7l": "arm",
    }

    # Determine the system architecture
    arch = arch_map.get(machine, machine)

    # Handle different operating systems
    if system == "windows":
        return f"windows-{arch}.exe"
    elif system == "darwin":
        return f"darwin-{arch}"
    elif system == "linux":
        return f"linux-{arch}"
    else:
        raise ValueError(f"Unsupported system: {system}")


def _get_latest_version() -> str:
    """
    Get the latest version of the Gateway API plugin from GitHub.
    """
    cmd = [
        "-s",
        "https://api.github.com/repos/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/releases/latest",
    ]
    try:
        result = run_command(command="curl", args=cmd)
        # Parse version from result
        version_match = re.search(r'"tag_name":\s*"v([^"]+)"', result)
        if version_match:
            return version_match.group(1)
        return "0.5.0"  # Default to latest known stable version if unable to fetch
    except Exception:
        return "0.5.0"  # Default to known stable version


def _check_plugin_logs(
    namespace: Annotated[str, "Namespace where Argo Rollouts is installed"] = "argo-rollouts",
    timeout: Annotated[Optional[int], "Timeout in seconds for log checking"] = 60,
) -> GatewayPluginStatus:
    """
    Check Argo Rollouts controller logs for plugin installation status.
    """
    cmd = ["logs", "-n", namespace, "-l", "app.kubernetes.io/name=argo-rollouts", "--tail", "100"]
    try:
        logs = run_command(command="kubectl", args=cmd)

        # Parse download information
        download_pattern = r'Downloading plugin argoproj-labs/gatewayAPI from: .*/v([\d.]+)/gatewayapi-plugin-([\w-]+)"'
        time_pattern = r"Download complete, it took ([\d.]+)s"

        version_match = re.search(download_pattern, logs)
        time_match = re.search(time_pattern, logs)

        if version_match and time_match:
            return GatewayPluginStatus(
                installed=True,
                version=version_match.group(1),
                architecture=version_match.group(2),
                download_time=float(time_match.group(1)),
            )

        return GatewayPluginStatus(installed=False, error_message="Plugin installation not found in logs")
    except Exception as e:
        return GatewayPluginStatus(installed=False, error_message=str(e))


# Create the function tools
verify_gateway_plugin = FunctionTool(
    _verify_gateway_plugin,
    description="Verify and configure Gateway API plugin for Argo Rollouts",
    name="verify_gateway_plugin",
)

check_plugin_logs = FunctionTool(
    _check_plugin_logs,
    description="Check Argo Rollouts controller logs for Gateway API plugin installation status",
    name="check_plugin_logs",
)

VerifyGatewayPluginTool, VerifyGatewayPluginToolConfig = create_typed_fn_tool(
    verify_gateway_plugin,
    "kagent.tools.argo.VerifyGatewayPluginTool",
    "VerifyGatewayPluginTool",
)

CheckPluginLogsTool, CheckPluginLogsToolConfig = create_typed_fn_tool(
    check_plugin_logs,
    "kagent.tools.argo.CheckPluginLogsTool",
    "CheckPluginLogsTool",
)
