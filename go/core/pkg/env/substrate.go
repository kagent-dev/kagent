package env

var (
	SubstrateATEAPIEndpoint       = RegisterStringVar("KAGENT_SUBSTRATE_ATE_API_ENDPOINT", "dns:///api.ate-system.svc:443", "Substrate control-plane gRPC endpoint.", ComponentController)
	SubstrateATEAPICAFile         = RegisterStringVar("KAGENT_SUBSTRATE_ATE_API_CA_FILE", "", "PEM CA bundle used to verify the Substrate API server. Empty uses system trust roots.", ComponentController)
	SubstrateATEAPIClientCertFile = RegisterStringVar("KAGENT_SUBSTRATE_ATE_API_CLIENT_CERT_FILE", "", "PEM bundle containing both the client certificate and private key for Substrate API mTLS. Reloaded for each TLS handshake.", ComponentController)
)

var SubstrateAtenetRouterURL = RegisterStringVar(
	"KAGENT_SUBSTRATE_ATENET_ROUTER_URL",
	"http://atenet-router.ate-system.svc:80",
	"Substrate router endpoint for agent and sandbox guest traffic.",
	ComponentController,
)
