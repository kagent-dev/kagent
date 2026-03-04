package agent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ConvertServiceToRemoteMCPServer(svc *corev1.Service) (*v1alpha2.RemoteMCPServer, error) {
	// Check wellknown annotations
	port := int64(0)
	protocol := string(MCPServiceProtocolDefault)
	path := MCPServicePathDefault
	if svc.Annotations != nil {
		if portStr, ok := svc.Annotations[MCPServicePortAnnotation]; ok {
			var err error
			port, err = strconv.ParseInt(portStr, 10, 64)
			if err != nil {
				return nil, NewValidationError("port in annotation %s is not a valid integer: %v", MCPServicePortAnnotation, err)
			}
		}
		if protocolStr, ok := svc.Annotations[MCPServiceProtocolAnnotation]; ok {
			if protocolStr != string(v1alpha2.RemoteMCPServerProtocolSse) && protocolStr != string(v1alpha2.RemoteMCPServerProtocolStreamableHttp) {
				// default to streamable http
				protocol = string(v1alpha2.RemoteMCPServerProtocolStreamableHttp)
			} else {
				protocol = protocolStr
			}
		}
		if pathStr, ok := svc.Annotations[MCPServicePathAnnotation]; ok {
			path = pathStr
		}
	}
	if port == 0 {
		if len(svc.Spec.Ports) == 1 {
			port = int64(svc.Spec.Ports[0].Port)
		} else {
			// Look through ports to find AppProtocol = mcp
			for _, svcPort := range svc.Spec.Ports {
				if svcPort.AppProtocol != nil && strings.ToLower(*svcPort.AppProtocol) == "mcp" {
					port = int64(svcPort.Port)
					break
				}
			}
		}
	}
	if port == 0 {
		return nil, NewValidationError("no port found for service %s with protocol %s", svc.Name, protocol)
	}
	return &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      fmt.Sprintf("http://%s.%s:%d%s", svc.Name, svc.Namespace, port, path),
			Protocol: v1alpha2.RemoteMCPServerProtocol(protocol),
		},
	}, nil
}

func ConvertMCPServerToRemoteMCPServer(mcpServer *v1alpha1.MCPServer) (*v1alpha2.RemoteMCPServer, error) {
	if mcpServer.Spec.Deployment.Port == 0 {
		return nil, NewValidationError("cannot determine port for MCP server %s", mcpServer.Name)
	}

	remoteMCP := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      fmt.Sprintf("http://%s.%s:%d/mcp", mcpServer.Name, mcpServer.Namespace, mcpServer.Spec.Deployment.Port),
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
	}

	// Propagate the timeout from the MCPServer CRD to the generated
	// RemoteMCPServer spec. Fall back to DefaultMCPServerTimeout for
	// MCPServer objects created before the CRD default was introduced,
	// so the ADK never uses its own 5s built-in which is too short for
	// sidecar gateway cold starts.
	if mcpServer.Spec.Timeout != nil {
		remoteMCP.Spec.Timeout = mcpServer.Spec.Timeout
	} else {
		remoteMCP.Spec.Timeout = &metav1.Duration{Duration: DefaultMCPServerTimeout}
	}

	return remoteMCP, nil
}
