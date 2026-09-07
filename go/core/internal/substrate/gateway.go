package substrate

// DefaultAtenetRouterURL is the in-cluster HTTP endpoint for Substrate's Envoy router.
const DefaultAtenetRouterURL = "http://atenet-router.ate-system.svc:80"

const defaultActorHostSuffix = "actors.resources.substrate.ate.dev"

// ActorHost returns the atenet-router Host header value for an actor.
func ActorHost(atespace, actorID, suffix string) string {
	if suffix == "" {
		suffix = defaultActorHostSuffix
	}
	return actorID + "." + atespace + "." + suffix
}
