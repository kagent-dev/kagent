package controller

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

// evaluateEgressGrant checks the complete candidate without reading persisted
// grants. On conflict, retain the raw union so preparation can apply removals.
func evaluateEgressGrant(resolution translator.EgressGrantResolution, base egress.Access, tokenOrigins []string) (egress.Access, []translator.EgressGrantIssue, bool) {
	candidate, err := egress.CanonicalAccess(resolution.Access.Destinations, resolution.Access.Credentials)
	if err == nil {
		_, err = egress.MergeAccess(base, candidate, tokenOrigins)
	}
	if err == nil {
		return candidate, resolution.Issues, false
	}
	issues := append(resolution.Issues, translator.EgressGrantIssue{
		Reason: v1alpha3.EgressPoliciesReasonConflict, Message: err.Error(),
	})
	return resolution.Access, issues, true
}

func retainedEgressGrant(previous, candidate, base egress.Access, tokenOrigins []string) egress.Access {
	retained := egress.IntersectAccess(previous, candidate)
	filtered, _ := egress.FilterConflicts(base, retained, tokenOrigins)
	return filtered
}

func egressPoliciesCondition(conditionType string, generation int64, issues []translator.EgressGrantIssue, conflict bool) metav1.Condition {
	condition := metav1.Condition{
		Type: conditionType, ObservedGeneration: generation,
		Status: metav1.ConditionTrue, Reason: v1alpha3.EgressPoliciesReasonApplied,
		Message: "All requested egress grants are admitted",
	}
	if len(issues) == 0 && !conflict {
		return condition
	}
	condition.Status = metav1.ConditionFalse
	var messages []string
	for _, issue := range issues {
		message := issue.Message
		if issue.Policy != "" {
			message = fmt.Sprintf("%s: %s", issue.Policy, message)
		}
		messages = append(messages, message)
	}
	if conflict {
		condition.Reason = v1alpha3.EgressPoliciesReasonConflict
		condition.Message = "new grants are held, previously applied grants remain in effect; "
	} else {
		condition.Reason = issues[0].Reason
		condition.Message = ""
	}
	condition.Message += strings.Join(messages, "; ")
	// Stay comfortably below the Kubernetes condition message limit.
	const maxMessage = 4096
	if len(condition.Message) > maxMessage {
		condition.Message = strings.ToValidUTF8(condition.Message[:maxMessage-3], "") + "..."
	}
	return condition
}

func (c *compiledTarget) egressBase() (egress.Access, []string) {
	if c == nil {
		return egress.Access{}, nil
	}
	return egress.Access{Destinations: c.Revision.EgressDestinations, Credentials: c.Revision.Credentials}, c.Revision.TokenPassthroughOrigins
}
