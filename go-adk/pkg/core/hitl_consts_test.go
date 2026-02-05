package core

import "testing"

func TestHITLConstants(t *testing.T) {
	// Test interrupt types
	if KAgentHitlInterruptTypeToolApproval != "tool_approval" {
		t.Errorf("KAgentHitlInterruptTypeToolApproval = %q, want %q", KAgentHitlInterruptTypeToolApproval, "tool_approval")
	}

	// Test decision types
	if KAgentHitlDecisionTypeKey != "decision_type" {
		t.Errorf("KAgentHitlDecisionTypeKey = %q, want %q", KAgentHitlDecisionTypeKey, "decision_type")
	}
	if KAgentHitlDecisionTypeApprove != "approve" {
		t.Errorf("KAgentHitlDecisionTypeApprove = %q, want %q", KAgentHitlDecisionTypeApprove, "approve")
	}
	if KAgentHitlDecisionTypeDeny != "deny" {
		t.Errorf("KAgentHitlDecisionTypeDeny = %q, want %q", KAgentHitlDecisionTypeDeny, "deny")
	}
	if KAgentHitlDecisionTypeReject != "reject" {
		t.Errorf("KAgentHitlDecisionTypeReject = %q, want %q", KAgentHitlDecisionTypeReject, "reject")
	}

	// Test resume keywords
	hasApproved := false
	hasProceed := false
	for _, keyword := range KAgentHitlResumeKeywordsApprove {
		if keyword == "approved" {
			hasApproved = true
		}
		if keyword == "proceed" {
			hasProceed = true
		}
	}
	if !hasApproved {
		t.Error("KAgentHitlResumeKeywordsApprove should contain 'approved'")
	}
	if !hasProceed {
		t.Error("KAgentHitlResumeKeywordsApprove should contain 'proceed'")
	}

	hasDenied := false
	hasCancel := false
	for _, keyword := range KAgentHitlResumeKeywordsDeny {
		if keyword == "denied" {
			hasDenied = true
		}
		if keyword == "cancel" {
			hasCancel = true
		}
	}
	if !hasDenied {
		t.Error("KAgentHitlResumeKeywordsDeny should contain 'denied'")
	}
	if !hasCancel {
		t.Error("KAgentHitlResumeKeywordsDeny should contain 'cancel'")
	}
}
