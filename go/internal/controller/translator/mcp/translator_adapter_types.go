package mcp

import (
	"net"
	"time"
)

// ============================================================================
// Core Types from local.rs
// ============================================================================

// LocalConfig represents the main configuration structure
type LocalConfig struct {
	Config    interface{}     `json:"config" yaml:"config"` // required type
	Binds     []LocalBind     `json:"binds,omitempty" yaml:"binds,omitempty"`
	Workloads []LocalWorkload `json:"workloads,omitempty" yaml:"workloads,omitempty"`
	Services  []Service       `json:"services,omitempty" yaml:"services,omitempty"`
}

// LocalBind represents a network bind configuration
type LocalBind struct {
	Port      uint16          `json:"port" yaml:"port"`
	Listeners []LocalListener `json:"listeners" yaml:"listeners"`
}

// LocalListener represents a listener configuration
type LocalListener struct {
	Name        string                `json:"name,omitempty" yaml:"name,omitempty"`
	GatewayName string                `json:"gatewayName,omitempty" yaml:"gatewayName,omitempty"`
	Hostname    string                `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	Protocol    LocalListenerProtocol `json:"protocol" yaml:"protocol"`
	TLS         *LocalTLSServerConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
	Routes      []LocalRoute          `json:"routes,omitempty" yaml:"routes,omitempty"`
	TCPRoutes   []LocalTCPRoute       `json:"tcpRoutes,omitempty" yaml:"tcpRoutes,omitempty"`
}

// LocalListenerProtocol represents the protocol type
type LocalListenerProtocol string

const (
	LocalListenerProtocolHTTP  LocalListenerProtocol = "HTTP"
	LocalListenerProtocolHTTPS LocalListenerProtocol = "HTTPS"
	LocalListenerProtocolTLS   LocalListenerProtocol = "TLS"
	LocalListenerProtocolTCP   LocalListenerProtocol = "TCP"
	LocalListenerProtocolHBONE LocalListenerProtocol = "HBONE"
)

// LocalTLSServerConfig represents TLS server configuration
type LocalTLSServerConfig struct {
	Cert string `json:"cert" yaml:"cert"`
	Key  string `json:"key" yaml:"key"`
}

// LocalRoute represents an HTTP route configuration
type LocalRoute struct {
	RouteName string          `json:"name,omitempty" yaml:"name,omitempty"`
	RuleName  string          `json:"ruleName,omitempty" yaml:"ruleName,omitempty"`
	Hostnames []string        `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`
	Matches   []RouteMatch    `json:"matches,omitempty" yaml:"matches,omitempty"`
	Policies  *FilterOrPolicy `json:"policies,omitempty" yaml:"policies,omitempty"`
	Backends  []RouteBackend  `json:"backends,omitempty" yaml:"backends,omitempty"`
}

// LocalTCPRoute represents a TCP route configuration
type LocalTCPRoute struct {
	RouteName string             `json:"name,omitempty" yaml:"name,omitempty"`
	RuleName  string             `json:"ruleName,omitempty" yaml:"ruleName,omitempty"`
	Hostnames []string           `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`
	Policies  *TCPFilterOrPolicy `json:"policies,omitempty" yaml:"policies,omitempty"`
	Backends  []TCPRouteBackend  `json:"backends,omitempty" yaml:"backends,omitempty"`
}

// LocalWorkload represents a local workload
type LocalWorkload struct {
	Workload Workload                     `json:",inline" yaml:",inline"`
	Services map[string]map[uint16]uint16 `json:"services,omitempty" yaml:"services,omitempty"`
}

// ============================================================================
// Policy and Filter Types
// ============================================================================

// FilterOrPolicy represents route filters and policies
type FilterOrPolicy struct {
	// Filters
	RequestHeaderModifier  *HeaderModifier  `json:"requestHeaderModifier,omitempty" yaml:"requestHeaderModifier,omitempty"`
	ResponseHeaderModifier *HeaderModifier  `json:"responseHeaderModifier,omitempty" yaml:"responseHeaderModifier,omitempty"`
	RequestRedirect        *RequestRedirect `json:"requestRedirect,omitempty" yaml:"requestRedirect,omitempty"`
	URLRewrite             *URLRewrite      `json:"urlRewrite,omitempty" yaml:"urlRewrite,omitempty"`
	RequestMirror          *RequestMirror   `json:"requestMirror,omitempty" yaml:"requestMirror,omitempty"`
	DirectResponse         *DirectResponse  `json:"directResponse,omitempty" yaml:"directResponse,omitempty"`
	CORS                   *CORS            `json:"cors,omitempty" yaml:"cors,omitempty"`

	// Policies
	MCPAuthorization *MCPAuthorization `json:"mcpAuthorization,omitempty" yaml:"mcpAuthorization,omitempty"`
	A2A              *A2APolicy        `json:"a2a,omitempty" yaml:"a2a,omitempty"`
	AI               interface{}       `json:"ai,omitempty" yaml:"ai,omitempty"` // Skipped complex type
	BackendTLS       *BackendTLS       `json:"backendTLS,omitempty" yaml:"backendTLS,omitempty"`
	BackendAuth      *BackendAuth      `json:"backendAuth,omitempty" yaml:"backendAuth,omitempty"`
	LocalRateLimit   []interface{}     `json:"localRateLimit,omitempty" yaml:"localRateLimit,omitempty"`   // Skipped complex type
	RemoteRateLimit  interface{}       `json:"remoteRateLimit,omitempty" yaml:"remoteRateLimit,omitempty"` // Skipped complex type
	JWTAuth          interface{}       `json:"jwtAuth,omitempty" yaml:"jwtAuth,omitempty"`                 // Skipped complex type
	ExtAuthz         interface{}       `json:"extAuthz,omitempty" yaml:"extAuthz,omitempty"`               // Skipped complex type

	// Traffic Policy
	Timeout *TimeoutPolicy `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retry   *RetryPolicy   `json:"retry,omitempty" yaml:"retry,omitempty"`
}

// TCPFilterOrPolicy represents TCP route policies
type TCPFilterOrPolicy struct {
	BackendTLS *BackendTLS `json:"backendTLS,omitempty" yaml:"backendTLS,omitempty"`
}

// ============================================================================
// Route and Backend Types from agent.rs
// ============================================================================

// RouteMatch represents route matching criteria
type RouteMatch struct {
	Headers []HeaderMatch `json:"headers,omitempty" yaml:"headers,omitempty"`
	Path    PathMatch     `json:"path" yaml:"path"`
	Method  *MethodMatch  `json:"method,omitempty" yaml:"method,omitempty"`
	Query   []QueryMatch  `json:"query,omitempty" yaml:"query,omitempty"`
}

// HeaderMatch represents header matching
type HeaderMatch struct {
	Name  string           `json:"name" yaml:"name"`
	Value HeaderValueMatch `json:"value" yaml:"value"`
}

// HeaderValueMatch represents header value matching
type HeaderValueMatch struct {
	Exact string `json:"exact,omitempty" yaml:"exact,omitempty"`
	Regex string `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// PathMatch represents path matching
type PathMatch struct {
	Exact      string `json:"exact,omitempty" yaml:"exact,omitempty"`
	PathPrefix string `json:"pathPrefix,omitempty" yaml:"pathPrefix,omitempty"`
	Regex      *struct {
		Pattern string `json:"pattern" yaml:"pattern"`
		Length  int    `json:"length" yaml:"length"`
	} `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// MethodMatch represents HTTP method matching
type MethodMatch struct {
	Method string `json:"method" yaml:"method"`
}

// QueryMatch represents query parameter matching
type QueryMatch struct {
	Name  string          `json:"name" yaml:"name"`
	Value QueryValueMatch `json:"value" yaml:"value"`
}

// QueryValueMatch represents query value matching
type QueryValueMatch struct {
	Exact string `json:"exact,omitempty" yaml:"exact,omitempty"`
	Regex string `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// RouteBackend represents a route backend
type RouteBackend struct {
	Weight  int             `json:"weight" yaml:"weight"`
	Service *ServiceBackend `json:"service,omitempty" yaml:"service,omitempty"`
	Opaque  *Target         `json:"opaque,omitempty" yaml:"opaque,omitempty"`
	Dynamic *struct{}       `json:"dynamic,omitempty" yaml:"dynamic,omitempty"`
	MCP     *MCPBackend     `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	AI      *AIBackend      `json:"ai,omitempty" yaml:"ai,omitempty"`
	Invalid bool            `json:"invalid,omitempty" yaml:"invalid,omitempty"`
	Filters []RouteFilter   `json:"filters,omitempty" yaml:"filters,omitempty"`
}

// TCPRouteBackend represents a TCP route backend
type TCPRouteBackend struct {
	Weight  int           `json:"weight" yaml:"weight"`
	Backend SimpleBackend `json:"backend" yaml:"backend"`
}

// SimpleBackend represents simpler backend types
type SimpleBackend struct {
	Service *ServiceBackend `json:"service,omitempty" yaml:"service,omitempty"`
	Opaque  *Target         `json:"opaque,omitempty" yaml:"opaque,omitempty"`
	Invalid bool            `json:"invalid,omitempty" yaml:"invalid,omitempty"`
}

// ServiceBackend represents a service backend
type ServiceBackend struct {
	Name NamespacedHostname `json:"name" yaml:"name"`
	Port uint16             `json:"port" yaml:"port"`
}

// Target represents a backend target
type Target struct {
	Address  *net.TCPAddr `json:"address,omitempty" yaml:"address,omitempty"`
	Hostname *struct {
		Host string `json:"host" yaml:"host"`
		Port uint16 `json:"port" yaml:"port"`
	} `json:"hostname,omitempty" yaml:"hostname,omitempty"`
}

// MCPBackend represents an MCP backend
type MCPBackend struct {
	Targets []MCPTarget `json:"targets" yaml:"targets"`
}

// MCPTarget represents an MCP target
type MCPTarget struct {
	Name    string             `json:"name" yaml:"name"`
	SSE     *SSETargetSpec     `json:"sse,omitempty" yaml:"sse,omitempty"`
	Stdio   *StdioTargetSpec   `json:"stdio,omitempty" yaml:"stdio,omitempty"`
	OpenAPI *OpenAPITargetSpec `json:"openapi,omitempty" yaml:"openapi,omitempty"`
	Filters []interface{}      `json:"filters,omitempty" yaml:"filters,omitempty"` // Skipped complex type
}

// SSETargetSpec represents SSE target specification
type SSETargetSpec struct {
	Host string `json:"host" yaml:"host"`
	Port uint32 `json:"port" yaml:"port"`
	Path string `json:"path" yaml:"path"`
}

// StdioTargetSpec represents stdio target specification
type StdioTargetSpec struct {
	Cmd  string            `json:"cmd" yaml:"cmd"`
	Args []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

// OpenAPITargetSpec represents OpenAPI target specification
type OpenAPITargetSpec struct {
	Host   string      `json:"host" yaml:"host"`
	Port   uint32      `json:"port" yaml:"port"`
	Schema interface{} `json:"schema" yaml:"schema"` // OpenAPI schema
}

// AIBackend represents an AI backend (placeholder)
type AIBackend struct {
	Name string `json:"name" yaml:"name"`
}

// RouteFilter represents route filters
type RouteFilter struct {
	RequestHeaderModifier  *HeaderModifier  `json:"requestHeaderModifier,omitempty" yaml:"requestHeaderModifier,omitempty"`
	ResponseHeaderModifier *HeaderModifier  `json:"responseHeaderModifier,omitempty" yaml:"responseHeaderModifier,omitempty"`
	RequestRedirect        *RequestRedirect `json:"requestRedirect,omitempty" yaml:"requestRedirect,omitempty"`
	URLRewrite             *URLRewrite      `json:"urlRewrite,omitempty" yaml:"urlRewrite,omitempty"`
	RequestMirror          *RequestMirror   `json:"requestMirror,omitempty" yaml:"requestMirror,omitempty"`
	DirectResponse         *DirectResponse  `json:"directResponse,omitempty" yaml:"directResponse,omitempty"`
	CORS                   *CORS            `json:"cors,omitempty" yaml:"cors,omitempty"`
}

// ============================================================================
// Filter Types
// ============================================================================

// HeaderModifier represents header modification
type HeaderModifier struct {
	Add    map[string]string `json:"add,omitempty" yaml:"add,omitempty"`
	Set    map[string]string `json:"set,omitempty" yaml:"set,omitempty"`
	Remove []string          `json:"remove,omitempty" yaml:"remove,omitempty"`
}

// RequestRedirect represents request redirection
type RequestRedirect struct {
	Scheme    string        `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Authority *HostRedirect `json:"authority,omitempty" yaml:"authority,omitempty"`
	Path      *PathRedirect `json:"path,omitempty" yaml:"path,omitempty"`
	Status    *int          `json:"status,omitempty" yaml:"status,omitempty"`
}

// URLRewrite represents URL rewriting
type URLRewrite struct {
	Authority *HostRedirect `json:"authority,omitempty" yaml:"authority,omitempty"`
	Path      *PathRedirect `json:"path,omitempty" yaml:"path,omitempty"`
}

// RequestMirror represents request mirroring
type RequestMirror struct {
	Backend    SimpleBackend `json:"backend" yaml:"backend"`
	Percentage float64       `json:"percentage" yaml:"percentage"`
}

// DirectResponse represents direct response
type DirectResponse struct {
	Status  int               `json:"status" yaml:"status"`
	Body    string            `json:"body,omitempty" yaml:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// CORS represents CORS configuration
type CORS struct {
	AllowOrigins     []string `json:"allowOrigins,omitempty" yaml:"allowOrigins,omitempty"`
	AllowMethods     []string `json:"allowMethods,omitempty" yaml:"allowMethods,omitempty"`
	AllowHeaders     []string `json:"allowHeaders,omitempty" yaml:"allowHeaders,omitempty"`
	ExposeHeaders    []string `json:"exposeHeaders,omitempty" yaml:"exposeHeaders,omitempty"`
	MaxAge           *int     `json:"maxAge,omitempty" yaml:"maxAge,omitempty"`
	AllowCredentials bool     `json:"allowCredentials,omitempty" yaml:"allowCredentials,omitempty"`
}

// HostRedirect represents host redirection
type HostRedirect struct {
	Full string  `json:"full,omitempty" yaml:"full,omitempty"`
	Host string  `json:"host,omitempty" yaml:"host,omitempty"`
	Port *uint16 `json:"port,omitempty" yaml:"port,omitempty"`
}

// PathRedirect represents path redirection
type PathRedirect struct {
	Full   string `json:"full,omitempty" yaml:"full,omitempty"`
	Prefix string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
}

// ============================================================================
// Policy Types
// ============================================================================

// MCPAuthorization represents MCP authorization policy
type MCPAuthorization struct {
	Rules interface{} `json:"rules" yaml:"rules"` // RuleSet - skipped complex type
}

// A2APolicy represents application-to-application policy
type A2APolicy struct {
	// Empty struct in Rust
}

// BackendTLS represents backend TLS configuration
type BackendTLS struct {
	// This would contain TLS configuration but is complex in Rust
	// Simplified representation
	Insecure     bool   `json:"insecure,omitempty" yaml:"insecure,omitempty"`
	InsecureHost bool   `json:"insecureHost,omitempty" yaml:"insecureHost,omitempty"`
	Cert         string `json:"cert,omitempty" yaml:"cert,omitempty"`
	Key          string `json:"key,omitempty" yaml:"key,omitempty"`
	Root         string `json:"root,omitempty" yaml:"root,omitempty"`
}

// BackendAuth represents backend authentication
type BackendAuth struct {
	// Placeholder for backend auth configuration
	Type   string      `json:"type" yaml:"type"`
	Config interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

// TimeoutPolicy represents timeout policy
type TimeoutPolicy struct {
	RequestTimeout        *time.Duration `json:"requestTimeout,omitempty" yaml:"requestTimeout,omitempty"`
	BackendRequestTimeout *time.Duration `json:"backendRequestTimeout,omitempty" yaml:"backendRequestTimeout,omitempty"`
}

// RetryPolicy represents retry policy
type RetryPolicy struct {
	Attempts      int           `json:"attempts" yaml:"attempts"`
	PerTryTimeout time.Duration `json:"perTryTimeout" yaml:"perTryTimeout"`
	RetryOn       []string      `json:"retryOn,omitempty" yaml:"retryOn,omitempty"`
}

// ============================================================================
// Discovery Types from discovery.rs
// ============================================================================

// Workload represents a workload in the mesh
type Workload struct {
	WorkloadIPs    []net.IP             `json:"workloadIps" yaml:"workloadIps"`
	Waypoint       *GatewayAddress      `json:"waypoint,omitempty" yaml:"waypoint,omitempty"`
	NetworkGateway *GatewayAddress      `json:"networkGateway,omitempty" yaml:"networkGateway,omitempty"`
	Protocol       InboundProtocol      `json:"protocol" yaml:"protocol"`
	NetworkMode    NetworkMode          `json:"networkMode" yaml:"networkMode"`
	UID            string               `json:"uid,omitempty" yaml:"uid,omitempty"`
	Name           string               `json:"name" yaml:"name"`
	Namespace      string               `json:"namespace" yaml:"namespace"`
	TrustDomain    string               `json:"trustDomain,omitempty" yaml:"trustDomain,omitempty"`
	ServiceAccount string               `json:"serviceAccount,omitempty" yaml:"serviceAccount,omitempty"`
	Network        string               `json:"network,omitempty" yaml:"network,omitempty"`
	WorkloadName   string               `json:"workloadName,omitempty" yaml:"workloadName,omitempty"`
	WorkloadType   string               `json:"workloadType,omitempty" yaml:"workloadType,omitempty"`
	CanonicalName  string               `json:"canonicalName,omitempty" yaml:"canonicalName,omitempty"`
	CanonicalRev   string               `json:"canonicalRevision,omitempty" yaml:"canonicalRevision,omitempty"`
	Hostname       string               `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	Node           string               `json:"node,omitempty" yaml:"node,omitempty"`
	AuthPolicies   []string             `json:"authorizationPolicies,omitempty" yaml:"authorizationPolicies,omitempty"`
	Status         HealthStatus         `json:"status" yaml:"status"`
	ClusterID      string               `json:"clusterId" yaml:"clusterId"`
	Locality       Locality             `json:"locality,omitempty" yaml:"locality,omitempty"`
	Services       []NamespacedHostname `json:"services,omitempty" yaml:"services,omitempty"`
	Capacity       uint32               `json:"capacity" yaml:"capacity"`
}

// Service represents a service in the mesh
type Service struct {
	Name            string                 `json:"name" yaml:"name"`
	Namespace       string                 `json:"namespace" yaml:"namespace"`
	Hostname        string                 `json:"hostname" yaml:"hostname"`
	VIPs            []NetworkAddress       `json:"vips" yaml:"vips"`
	Ports           map[uint16]uint16      `json:"ports" yaml:"ports"`
	AppProtocols    map[uint16]AppProtocol `json:"appProtocols,omitempty" yaml:"appProtocols,omitempty"`
	Endpoints       map[string]Endpoint    `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	SubjectAltNames []string               `json:"subjectAltNames,omitempty" yaml:"subjectAltNames,omitempty"`
	Waypoint        *GatewayAddress        `json:"waypoint,omitempty" yaml:"waypoint,omitempty"`
	LoadBalancer    *LoadBalancer          `json:"loadBalancer,omitempty" yaml:"loadBalancer,omitempty"`
	IPFamilies      *IPFamily              `json:"ipFamilies,omitempty" yaml:"ipFamilies,omitempty"`
}

// NamespacedHostname represents a hostname within a namespace
type NamespacedHostname struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	Hostname  string `json:"hostname" yaml:"hostname"`
}

// NetworkAddress represents an address on a specific network
type NetworkAddress struct {
	Network string `json:"network" yaml:"network"`
	Address net.IP `json:"address" yaml:"address"`
}

// GatewayAddress represents a gateway address
type GatewayAddress struct {
	Destination   GatewayDestination `json:"destination" yaml:"destination"`
	HBONEMTLSPort uint16             `json:"hboneMtlsPort" yaml:"hboneMtlsPort"`
}

// GatewayDestination represents gateway destination
type GatewayDestination struct {
	Address  *NetworkAddress     `json:"address,omitempty" yaml:"address,omitempty"`
	Hostname *NamespacedHostname `json:"hostname,omitempty" yaml:"hostname,omitempty"`
}

// Endpoint represents a service endpoint
type Endpoint struct {
	WorkloadUID string            `json:"workloadUid" yaml:"workloadUid"`
	Port        map[uint16]uint16 `json:"port" yaml:"port"`
	Status      HealthStatus      `json:"status" yaml:"status"`
}

// LoadBalancer represents load balancer configuration
type LoadBalancer struct {
	RoutingPreferences []LoadBalancerScope      `json:"routingPreferences" yaml:"routingPreferences"`
	Mode               LoadBalancerMode         `json:"mode" yaml:"mode"`
	HealthPolicy       LoadBalancerHealthPolicy `json:"healthPolicy" yaml:"healthPolicy"`
}

// Locality represents geographical locality
type Locality struct {
	Region  string `json:"region" yaml:"region"`
	Zone    string `json:"zone" yaml:"zone"`
	Subzone string `json:"subzone" yaml:"subzone"`
}

// Identity represents a workload identity
type Identity struct {
	TrustDomain    string `json:"trustDomain" yaml:"trustDomain"`
	Namespace      string `json:"namespace" yaml:"namespace"`
	ServiceAccount string `json:"serviceAccount" yaml:"serviceAccount"`
}

// ============================================================================
// Enums and Constants
// ============================================================================

// InboundProtocol represents the inbound protocol
type InboundProtocol string

const (
	InboundProtocolTCP             InboundProtocol = "TCP"
	InboundProtocolHBONE           InboundProtocol = "HBONE"
	InboundProtocolLegacyIstioMTLS InboundProtocol = "LegacyIstioMtls"
)

// OutboundProtocol represents the outbound protocol
type OutboundProtocol string

const (
	OutboundProtocolTCP         OutboundProtocol = "TCP"
	OutboundProtocolHBONE       OutboundProtocol = "HBONE"
	OutboundProtocolDoubleHBONE OutboundProtocol = "DOUBLEHBONE"
)

// NetworkMode represents the network mode
type NetworkMode string

const (
	NetworkModeStandard    NetworkMode = "Standard"
	NetworkModeHostNetwork NetworkMode = "HostNetwork"
)

// HealthStatus represents health status
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "Healthy"
	HealthStatusUnhealthy HealthStatus = "Unhealthy"
)

// AppProtocol represents application protocol
type AppProtocol string

const (
	AppProtocolHTTP11 AppProtocol = "Http11"
	AppProtocolHTTP2  AppProtocol = "Http2"
	AppProtocolGRPC   AppProtocol = "Grpc"
)

// IPFamily represents IP family
type IPFamily string

const (
	IPFamilyDual IPFamily = "Dual"
	IPFamilyIPv4 IPFamily = "IPv4"
	IPFamilyIPv6 IPFamily = "IPv6"
)

// LoadBalancerScope represents load balancer scope
type LoadBalancerScope string

const (
	LoadBalancerScopeRegion  LoadBalancerScope = "Region"
	LoadBalancerScopeZone    LoadBalancerScope = "Zone"
	LoadBalancerScopeSubzone LoadBalancerScope = "Subzone"
	LoadBalancerScopeNode    LoadBalancerScope = "Node"
	LoadBalancerScopeCluster LoadBalancerScope = "Cluster"
	LoadBalancerScopeNetwork LoadBalancerScope = "Network"
)

// LoadBalancerMode represents load balancer mode
type LoadBalancerMode string

const (
	LoadBalancerModeStandard LoadBalancerMode = "Standard"
	LoadBalancerModeStrict   LoadBalancerMode = "Strict"
	LoadBalancerModeFailover LoadBalancerMode = "Failover"
)

// LoadBalancerHealthPolicy represents load balancer health policy
type LoadBalancerHealthPolicy string

const (
	LoadBalancerHealthPolicyOnlyHealthy LoadBalancerHealthPolicy = "OnlyHealthy"
	LoadBalancerHealthPolicyAllowAll    LoadBalancerHealthPolicy = "AllowAll"
)
