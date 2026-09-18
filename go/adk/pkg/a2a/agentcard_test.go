package a2a

import (
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
)

const secretInstruction = "SECRET-RULE: escalate refunds above 500 to a human."

func cardSkillsText(card *a2atype.AgentCard) string {
	var sb strings.Builder
	for _, skill := range card.Skills {
		sb.WriteString(skill.ID + " " + skill.Name + " " + skill.Description + "\n")
	}
	return sb.String()
}

func newSupportAgent(t *testing.T, subAgents ...adkagent.Agent) adkagent.Agent {
	t.Helper()
	agent, err := llmagent.New(llmagent.Config{
		Name:        "support",
		Description: "Handles support tickets.",
		Instruction: secretInstruction,
		SubAgents:   subAgents,
	})
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestEnrichAgentCard_NeverDerivesSkillsFromAgent(t *testing.T) {
	billing, err := llmagent.New(llmagent.Config{
		Name:        "billing",
		Description: "Looks up invoices.",
		Instruction: "You are the billing agent.",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := newSupportAgent(t, billing)

	// A card without skills stays without skills: nothing is derived from the
	// agent, so its instructions cannot reach the discovery endpoint.
	card := &a2atype.AgentCard{Name: "support"}
	EnrichAgentCard(card, agent)

	if len(card.Skills) != 0 {
		t.Fatalf("skills were derived from the agent: %s", cardSkillsText(card))
	}
	if strings.Contains(card.Description, "SECRET-RULE") {
		t.Fatalf("description leaks instructions: %q", card.Description)
	}
	if card.Description != "Handles support tickets." {
		t.Fatalf("description = %q, want agent description", card.Description)
	}
	if !hasHITLExtension(card.Capabilities.Extensions) || len(card.SupportedInterfaces) != 1 {
		t.Fatalf("existing enrichment (HITL extension, default interface) regressed: %+v", card)
	}
}

func TestEnrichAgentCard_KeepsCompilerGeneratedSkills(t *testing.T) {
	agent := newSupportAgent(t)

	curated := []a2atype.AgentSkill{{ID: "refunds", Name: "Refunds", Description: "Processes refund requests."}}
	card := &a2atype.AgentCard{Name: "support", Description: "Curated description.", Skills: curated}
	EnrichAgentCard(card, agent)

	if len(card.Skills) != 1 || card.Skills[0].ID != "refunds" || card.Skills[0].Description != "Processes refund requests." {
		t.Fatalf("compiler-generated skills were changed: %s", cardSkillsText(card))
	}
	if card.Description != "Curated description." {
		t.Fatalf("description = %q, want the card's own description kept", card.Description)
	}
}
