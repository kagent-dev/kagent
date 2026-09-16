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

func TestEnrichAgentCard_DerivedSkillsNeverEmbedInstructions(t *testing.T) {
	billing, err := llmagent.New(llmagent.Config{
		Name:        "billing",
		Description: "Looks up invoices.",
		Instruction: "You are the billing agent.",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := newSupportAgent(t, billing)

	// A curated card without a skills field: skills are derived.
	card := &a2atype.AgentCard{Name: "support"}
	EnrichAgentCard(card, agent)

	text := cardSkillsText(card)
	if strings.Contains(text, "SECRET-RULE") || strings.Contains(text, "billing agent") {
		t.Fatalf("derived skills leak instructions:\n%s", text)
	}
	if len(card.Skills) != 2 {
		t.Fatalf("expected primary + sub-agent skill, got:\n%s", text)
	}
	if card.Skills[0].ID != "support" || card.Skills[0].Description != "Handles support tickets." {
		t.Fatalf("unexpected primary skill:\n%s", text)
	}
	if card.Skills[1].Name != "billing" || card.Skills[1].Description != "Looks up invoices." {
		t.Fatalf("unexpected sub-agent skill:\n%s", text)
	}
	if card.Description != "Handles support tickets." {
		t.Fatalf("description = %q, want agent description", card.Description)
	}
}

func TestEnrichAgentCard_KeepsCuratedSkills(t *testing.T) {
	agent := newSupportAgent(t)

	curated := []a2atype.AgentSkill{{ID: "refunds", Name: "Refunds", Description: "Processes refund requests."}}
	card := &a2atype.AgentCard{Name: "support", Skills: curated}
	EnrichAgentCard(card, agent)
	if len(card.Skills) != 1 || card.Skills[0].ID != "refunds" {
		t.Fatalf("curated skills were replaced: %s", cardSkillsText(card))
	}

	// An explicit empty list means "no skills" and must not be filled in.
	card = &a2atype.AgentCard{Name: "support", Skills: []a2atype.AgentSkill{}}
	EnrichAgentCard(card, agent)
	if len(card.Skills) != 0 {
		t.Fatalf("explicit empty skills were filled: %s", cardSkillsText(card))
	}
	if !hasHITLExtension(card.Capabilities.Extensions) || len(card.SupportedInterfaces) != 1 {
		t.Fatalf("existing enrichment (HITL extension, default interface) regressed: %+v", card)
	}
}
