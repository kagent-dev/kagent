package substrate

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// DefaultAtenetRouterURL is the in-cluster HTTP endpoint for Substrate's router.
const DefaultAtenetRouterURL = "http://atenet-router.ate-system.svc:80"

const defaultActorHostSuffix = "actors.resources.substrate.ate.dev"

// ActorHost returns the logical A2A authority stored for an actor.
func ActorHost(atespace, actorID, suffix string) string {
	if suffix == "" {
		suffix = defaultActorHostSuffix
	}
	return actorID + "." + atespace + "." + suffix
}

// ActorTargetFromHost converts a stored A2A authority to Substrate's
// ate-target-actor header value. The authority itself is no longer routable.
func ActorTargetFromHost(authority string) (string, error) {
	name, ok := strings.CutSuffix(authority, "."+defaultActorHostSuffix)
	actor, atespace, hasAtespace := strings.Cut(name, ".")
	if !ok || !hasAtespace || len(validation.IsDNS1123Label(actor)) != 0 || len(validation.IsDNS1123Label(atespace)) != 0 {
		return "", fmt.Errorf("invalid runtime authority %q", authority)
	}
	return atespace + "/" + actor, nil
}
