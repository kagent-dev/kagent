package status

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileMCPServerStatus reconciles the status of an MCPServer based on the deployment state
func ReconcileMCPServerStatus(ctx context.Context, kube client.Client, mcpServer *v1alpha1.MCPServer, reconcileErr error) (bool, error) {
	// Set Accepted condition based on reconcile error
	setAcceptedCondition(mcpServer, reconcileErr)

	// Set ResolvedRefs condition (always true for now as we don't have complex refs)
	setResolvedRefsCondition(mcpServer, true, v1alpha1.MCPServerReasonResolvedRefs, "All references resolved")

	// Set Programmed condition based on whether resources were created successfully
	setProgrammedCondition(mcpServer, reconcileErr == nil)

	// Set Ready condition based on deployment status
	shouldRequeue := checkReadyCondition(ctx, kube, mcpServer)

	// Update the status if it has changed
	return shouldRequeue, updateMCPServerStatus(ctx, kube, mcpServer)
}

// setAcceptedCondition sets the Accepted condition on the MCPServer
func setAcceptedCondition(mcpServer *v1alpha1.MCPServer, err error) {
	status := metav1.ConditionTrue
	reason := v1alpha1.MCPServerReasonAccepted
	message := "MCPServer has been accepted for processing"

	if err != nil {
		status = metav1.ConditionFalse
		reason = v1alpha1.MCPServerReasonInvalidConfig
		message = fmt.Sprintf("MCPServer configuration is invalid: %v", err)
	}

	setCondition(mcpServer, v1alpha1.MCPServerConditionAccepted, status, reason, message)
}

// setResolvedRefsCondition sets the ResolvedRefs condition on the MCPServer
func setResolvedRefsCondition(mcpServer *v1alpha1.MCPServer, resolved bool, reason v1alpha1.MCPServerConditionReason, message string) {
	status := metav1.ConditionTrue
	if !resolved {
		status = metav1.ConditionFalse
	}
	setCondition(mcpServer, v1alpha1.MCPServerConditionResolvedRefs, status, reason, message)
}

// setProgrammedCondition sets the Programmed condition on the MCPServer
func setProgrammedCondition(mcpServer *v1alpha1.MCPServer, programmed bool) {
	status := metav1.ConditionTrue
	reason := v1alpha1.MCPServerReasonProgrammed
	message := "MCPServer has been successfully programmed"

	if !programmed {
		status = metav1.ConditionFalse
		reason = v1alpha1.MCPServerReasonDeploymentFailed
		message = "Failed to program MCPServer resources"
	}

	setCondition(mcpServer, v1alpha1.MCPServerConditionProgrammed, status, reason, message)
}

// checkReadyCondition checks if the MCPServer is ready by examining the deployment status
// returns true if the deployment is not ready and request should be requeued
func checkReadyCondition(ctx context.Context, kube client.Client, mcpServer *v1alpha1.MCPServer) bool {
	deployment := &appsv1.Deployment{}
	deploymentName := mcpServer.Name
	if err := kube.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: mcpServer.Namespace}, deployment); err != nil {
		if client.IgnoreNotFound(err) == nil {
			setReadyCondition(mcpServer, false, v1alpha1.MCPServerReasonPodsNotReady, "Deployment not found")
		} else {
			setReadyCondition(
				mcpServer,
				false,
				v1alpha1.MCPServerReasonPodsNotReady,
				fmt.Sprintf("Error getting deployment: %s", err.Error()),
			)
		}
		return false
	}

	if deployment.Status.AvailableReplicas > 0 && deployment.Status.AvailableReplicas == deployment.Status.Replicas {
		setReadyCondition(
			mcpServer,
			true,
			v1alpha1.MCPServerReasonAvailable,
			"Deployment is ready and all pods are running",
		)
	} else {
		message := fmt.Sprintf("Deployment not ready: %d/%d replicas available",
			deployment.Status.AvailableReplicas, deployment.Status.Replicas)
		setReadyCondition(mcpServer, false, v1alpha1.MCPServerReasonNotAvailable, message)
		return true
	}
	return false
}

// setReadyCondition sets the Ready condition on the MCPServer
func setReadyCondition(mcpServer *v1alpha1.MCPServer, ready bool, reason v1alpha1.MCPServerConditionReason, message string) {
	status := metav1.ConditionTrue
	if !ready {
		status = metav1.ConditionFalse
	}
	setCondition(mcpServer, v1alpha1.MCPServerConditionReady, status, reason, message)
}

// setCondition sets the given condition on the MCPServer status
func setCondition(
	mcpServer *v1alpha1.MCPServer,
	conditionType v1alpha1.MCPServerConditionType,
	status metav1.ConditionStatus,
	reason v1alpha1.MCPServerConditionReason,
	message string,
) {
	now := metav1.Now()
	condition := metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		LastTransitionTime: now,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: mcpServer.Generation,
	}

	// Find existing condition
	for i, existingCondition := range mcpServer.Status.Conditions {
		if existingCondition.Type == string(conditionType) {
			// Only update LastTransitionTime if status changed
			if existingCondition.Status != status {
				mcpServer.Status.Conditions[i] = condition
			} else {
				// Update other fields but keep the original LastTransitionTime
				condition.LastTransitionTime = existingCondition.LastTransitionTime
				mcpServer.Status.Conditions[i] = condition
			}
			return
		}
	}

	// Add new condition
	mcpServer.Status.Conditions = append(mcpServer.Status.Conditions, condition)
}

// updateMCPServerStatus updates the MCPServer status if it has changed
func updateMCPServerStatus(ctx context.Context, kube client.Client, mcpServer *v1alpha1.MCPServer) error {
	// Update observed generation
	mcpServer.Status.ObservedGeneration = mcpServer.Generation

	// Update the status
	if err := kube.Status().Update(ctx, mcpServer); err != nil {
		return fmt.Errorf("failed to update MCPServer status: %v", err)
	}

	return nil
}
