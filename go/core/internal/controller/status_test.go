package controller

import (
	"testing"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestStatusForPairPublishesCompilationWarnings(t *testing.T) {
	warnings := []string{"partial MCP selection is not enforced"}
	state := AgentReconciliation{
		Agent:      &kagentv1alpha3.Agent{},
		Revision:   &v2translator.Revision{},
		Warnings:   warnings,
		RevisionID: v2translator.RevisionID{1},
	}
	status := statusForAgent(state, 1, "")
	if len(status.Warnings) != 1 || status.Warnings[0] != warnings[0] {
		t.Fatalf("warnings = %v, want %v", status.Warnings, warnings)
	}
	warnings[0] = "changed"
	if status.Warnings[0] == warnings[0] {
		t.Fatal("status aliases mutable compilation warnings")
	}
}
