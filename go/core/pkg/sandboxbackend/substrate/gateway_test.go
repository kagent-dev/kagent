package substrate

import (
	"testing"
)

func TestGatewayRouterTarget(t *testing.T) {
	t.Parallel()
	target, host, err := GatewayRouterTarget("", "kagent", "ahr-kagent-my-claw")
	if err != nil {
		t.Fatal(err)
	}
	if target.String() != DefaultAtenetRouterURL {
		t.Fatalf("target = %s, want %s", target, DefaultAtenetRouterURL)
	}
	if host != "ahr-kagent-my-claw.kagent.actors.resources.substrate.ate.dev" {
		t.Fatalf("host = %q", host)
	}
}

func TestGatewayRouterTargetCustomURL(t *testing.T) {
	t.Parallel()
	target, host, err := GatewayRouterTarget("http://atenet-router.custom.svc:8080", "kagent", "actor-1")
	if err != nil {
		t.Fatal(err)
	}
	if target.Host != "atenet-router.custom.svc:8080" {
		t.Fatalf("target host = %q", target.Host)
	}
	if host != "actor-1.kagent.actors.resources.substrate.ate.dev" {
		t.Fatalf("host = %q", host)
	}
}

func TestGatewayRouterTargetRejectsEmptyActor(t *testing.T) {
	t.Parallel()
	_, _, err := GatewayRouterTarget("", "kagent", "")
	if err == nil {
		t.Fatal("expected error for empty actor id")
	}
}

func TestGatewayRouterTargetRejectsEmptyAtespace(t *testing.T) {
	t.Parallel()
	_, _, err := GatewayRouterTarget("", "", "actor-1")
	if err == nil {
		t.Fatal("expected error for empty atespace")
	}
}
