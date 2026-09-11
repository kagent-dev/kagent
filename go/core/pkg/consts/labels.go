package consts

// DiscoveryLabel opts a tool server out of the controller's tool discovery. A
// RemoteMCPServer labeled `kagent.dev/discovery=disabled` is Accepted without the
// controller listing its tools: its status publishes no discovered tools, and the
// agents that reference it resolve the tool list at run time with the credentials
// they carry — for a server that authenticates every caller (a propagated caller
// token, which the controller does not hold), the controller-side listing would
// otherwise fail and mark the server as not Accepted forever. A kmcp MCPServer
// with the same label is left out of the catalog entirely: the controller neither
// lists its tools nor keeps a projection of it (the documented setup where
// agentgateway fronts the server and agents reach it through a RemoteMCPServer).
// Any other value, or no label, keeps discovery on.
const DiscoveryLabel = "kagent.dev/discovery"

// DiscoveryDisabled is the DiscoveryLabel value that turns tool discovery off.
const DiscoveryDisabled = "disabled"
