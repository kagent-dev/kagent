# KAgent Tools - Go Implementation

This directory contains the Go implementation of all KAgent tools, migrated from the original Python implementation. The tools are designed to work with the Model Context Protocol (MCP) server and provide comprehensive Kubernetes, cloud-native, and observability functionality.

## Architecture

The Go tools are implemented as a single MCP server that exposes all available tools through the MCP protocol. 
Each tool category is implemented in its own Go file for better organization and maintainability.

## Tool Categories

### 1. Kubernetes Tools (`k8s.go`)
Provides comprehensive Kubernetes cluster management functionality:

- **kubectl_get**: Get Kubernetes resources
- **kubectl_describe**: Describe Kubernetes resources in detail
- **kubectl_logs**: Get logs from pods
- **kubectl_scale**: Scale deployments and replica sets
- **kubectl_patch**: Patch Kubernetes resources
- **kubectl_label**: Add/remove labels from resources
- **kubectl_annotate**: Add/remove annotations from resources
- **kubectl_delete**: Delete Kubernetes resources
- **kubectl_apply**: Apply configurations from files or stdin
- **kubectl_create**: Create resources from files or stdin
- **check_service_connectivity**: Test service connectivity
- **get_events**: Get cluster events
- **get_api_resources**: List available API resources
- **get_cluster_configuration**: Get cluster configuration
- **exec_command**: Execute commands in pods
- **rollout**: Manage deployment rollouts

### 2. Helm Tools (`helm.go`)
Provides Helm package manager functionality:

- **helm_list**: List Helm releases
- **helm_get**: Get information about Helm releases
- **helm_upgrade**: Upgrade Helm releases
- **helm_uninstall**: Uninstall Helm releases
- **helm_install**: Install Helm charts
- **helm_repo_add**: Add Helm repositories
- **helm_repo_update**: Update Helm repositories

### 3. Istio Tools (`istio.go`)
Provides Istio service mesh management:

- **istio_proxy_status**: Get proxy status
- **istio_proxy_config**: Get proxy configuration
- **istio_install**: Install Istio
- **istio_generate_manifest**: Generate Istio manifests
- **istio_analyze**: Analyze Istio configuration
- **istio_version**: Get Istio version information
- **istio_remote_clusters**: Manage remote clusters
- **istio_waypoint_list**: List waypoint proxies
- **istio_waypoint_generate**: Generate waypoint proxy configuration
- **istio_waypoint_apply**: Apply waypoint proxy configuration
- **istio_waypoint_delete**: Delete waypoint proxies
- **istio_waypoint_status**: Get waypoint proxy status
- **istio_ztunnel_config**: Get ztunnel configuration

### 4. Argo Rollouts Tools (`argo.go`)
Provides Argo Rollouts progressive delivery functionality:

- **verify_argo_rollouts_controller_install**: Verify controller installation
- **verify_kubectl_plugin_install**: Verify kubectl plugin installation
- **promote_rollout**: Promote rollouts
- **pause_rollout**: Pause rollouts
- **set_rollout_image**: Set rollout images
- **verify_gateway_plugin**: Verify Gateway API plugin
- **check_plugin_logs**: Check plugin installation logs

### 5. Cilium Tools (`cilium.go`)
Provides Cilium CNI and networking functionality:

- **cilium_status_and_version**: Get Cilium status and version
- **upgrade_cilium**: Upgrade Cilium installation
- **install_cilium**: Install Cilium
- **uninstall_cilium**: Uninstall Cilium
- **connect_to_remote_cluster**: Connect to remote clusters
- **disconnect_remote_cluster**: Disconnect from remote clusters
- **list_bgp_peers**: List BGP peers
- **list_bgp_routes**: List BGP routes
- **show_cluster_mesh_status**: Show cluster mesh status
- **show_features_status**: Show Cilium features status
- **toggle_hubble**: Enable/disable Hubble
- **toggle_cluster_mesh**: Enable/disable cluster mesh

### 6. Prometheus Tools (`prometheus.go`)
Provides Prometheus monitoring and alerting functionality:

- **prometheus_query**: Execute PromQL queries
- **prometheus_range_query**: Execute PromQL range queries
- **prometheus_labels**: Get available labels
- **prometheus_targets**: Get scraping targets and their status

### 7. Grafana Tools (`grafana.go`)
Provides Grafana dashboard and alerting management:

- **grafana_org_management**: Manage Grafana organizations
- **grafana_dashboard_management**: Manage dashboards
- **grafana_alert_management**: Manage alerts and alert rules
- **grafana_datasource_management**: Manage data sources

### 8. DateTime Tools (`datetime.go`)
Provides time and date utilities:

- **current_date_time**: Get current date and time in ISO 8601 format
- **format_time**: Format timestamps with optional timezone
- **parse_time**: Parse time strings into RFC3339 format

### 9. Documentation Tools (`docs.go`)
Provides documentation query functionality:

- **query_documentation**: Query documentation for supported products (simplified implementation)
- **list_supported_products**: List supported products for documentation queries

### 10. Common Tools (`common.go`)
Provides general utility functions:

- **shell**: Execute shell commands

## Building and Running

### Prerequisites
- Go 1.21 or later
- Access to Kubernetes cluster (for K8s tools)
- Required CLI tools installed:
  - `kubectl` (for Kubernetes tools)
  - `helm` (for Helm tools)
  - `istioctl` (for Istio tools)
  - `cilium` (for Cilium tools)

### Building
```bash
go build -o kagent-tools .
```

### Running
```bash
./kagent-tools
```

The server runs using sse transport for MCP communication.

### Testing
```bash
go test -v
```

## Tool Implementation Details

### Error Handling
All tools implement comprehensive error handling and return appropriate error messages through the MCP protocol. When CLI tools are not available or commands fail, the tools return descriptive error messages.

### Authentication and Configuration
Tools respect existing authentication and configuration:
- Kubernetes tools use the default kubeconfig or `KUBECONFIG` environment variable
- Helm tools use Helm's default configuration
- Prometheus tools accept custom Prometheus server URLs
- Grafana tools support API key and basic authentication

### Command Execution
The tools use a common `runCommand` function that:
- Executes commands with proper error handling
- Captures both stdout and stderr
- Returns formatted output or error messages
- Handles timeouts and cancellation

### MCP Integration
All tools are properly integrated with the MCP protocol:
- Use proper parameter parsing with `mcp.ParseString`, `mcp.ParseBool`, etc.
- Return results using `mcp.NewToolResultText` or `mcp.NewToolResultError`
- Include comprehensive tool descriptions and parameter documentation
- Support required and optional parameters

## Migration from Python

This Go implementation provides feature parity with the original Python tools while offering:

1. **Better Performance**: Native Go execution without Python interpreter overhead
2. **Smaller Binary**: Single compiled binary with all tools included
3. **Better Resource Usage**: Lower memory footprint and faster startup
4. **Enhanced Error Handling**: More robust error handling and reporting
5. **Simplified Deployment**: No Python dependencies or virtual environments required

### Key Differences from Python Implementation
- Uses native Go clients instead of Python requests/httpx
- Implements simplified documentation query (full vector search would require additional Go libraries)
- Uses Go's native JSON handling instead of Python's json module
- Command execution uses Go's `os/exec` package instead of Python's subprocess

## Configuration

Tools can be configured through environment variables:
- `KUBECONFIG`: Kubernetes configuration file path
- `PROMETHEUS_URL`: Default Prometheus server URL
- `GRAFANA_URL`: Default Grafana server URL
- `GRAFANA_API_KEY`: Default Grafana API key

## Error Handling and Debugging

The tools provide detailed error messages and support verbose output. When debugging issues:

1. Check that required CLI tools are installed and in PATH
2. Verify authentication and configuration (kubeconfig, API keys, etc.)
3. Check network connectivity to target services
4. Review error messages for specific failure details

## Future Enhancements

Potential areas for future improvement:
1. **Native Client Libraries**: Replace CLI calls with native Go client libraries where possible
2. **Advanced Documentation Search**: Implement full vector search for documentation queries
3. **Caching**: Add caching for frequently accessed data
4. **Metrics and Observability**: Add metrics and tracing for tool usage
5. **Configuration Management**: Enhanced configuration management and validation
6. **Parallel Execution**: Support for parallel execution of related operations

## Contributing

When adding new tools or modifying existing ones:
1. Follow the existing code structure and naming conventions
2. Add comprehensive error handling
3. Include proper MCP tool registration
4. Update this README with new tool documentation
5. Add appropriate tests
6. Ensure backward compatibility with existing tools
