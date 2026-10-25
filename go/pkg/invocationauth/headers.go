package invocationauth

// HeadersMetadataKey carries resolved header bindings on the private A2A call.
// This is a JSON snapshot in binary gRPC metadata, never a credential reference.
const HeadersMetadataKey = "x-kagent-invocation-headers-bin"

// Target contains the headers admitted for one origin when the invocation starts.
// Headers contains injection placeholders; caller tokens remain separate.
type Target struct {
	Origin      string            `json:"origin"`
	Headers     map[string]string `json:"headers,omitempty"`
	Passthrough []Header          `json:"passthrough,omitempty"`
}

// Header formats the invocation credential for one outbound header.
type Header struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix,omitempty"`
}
