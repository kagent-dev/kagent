package env

var SubstrateAtenetRouterURL = RegisterStringVar(
	"SUBSTRATE_ATENET_ROUTER_URL",
	"http://atenet-router.ate-system.svc:80",
	"Substrate router endpoint for agent and sandbox guest traffic.",
	ComponentController,
)
