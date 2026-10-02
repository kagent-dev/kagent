package consts

// DiscoveryLabel opts a tool server out of the controller's tool discovery. A
// RemoteMCPServer labeled `kagent.dev/discovery=disabled` is Accepted without the
// controller listing its tools: its status publishes no discovered tools, and the
// agents that reference it resolve the tool list at run time with the credentials
// they carry — for a server that authenticates every caller (a propagated caller
// token, which the controller does not hold), the controller-side listing would
// otherwise fail and mark the server as not Accepted forever. Any other value, or
// no label, keeps discovery on. The same label excludes a Service or MCPServer
// from automatic discovery (predicates.DiscoveryDisabledPredicate).
//
// Accepted=True with reason DiscoveryDisabled means only that the controller
// skipped discovery on purpose and the spec.tls configuration is usable; it
// does not validate runtime authentication or endpoint reachability. When
// switching a server to the propagated caller credential, clear any
// spec.headersFrom entry that sets Authorization: it overrides the caller's
// token. Under server-side apply or GitOps tooling that only adds fields,
// omitting the entry may leave it in place, so set `headersFrom: []` explicitly.
const DiscoveryLabel = "kagent.dev/discovery"

// DiscoveryDisabled is the DiscoveryLabel value that turns discovery off.
const DiscoveryDisabled = "disabled"
