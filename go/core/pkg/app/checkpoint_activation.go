package app

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	checkpointClaimsAnnotation = "kagent.dev/checkpoint-execution-claims"
	checkpointClaimsVersion    = "v1"
	controllerComponentLabel   = "app.kubernetes.io/component"
)

// This barrier runs on every API replica, independently of leader election and
// readiness. A replacement can serve other API traffic and become Ready while
// the Deployment finishes rolling its old replicas out. Only then can recovery
// or new checkpoint mutations start. Use the live API reader, never the cache.
type checkpointActivation struct {
	reader             client.Reader
	namespace, podName string
	podUID             types.UID
	ready              chan struct{}
}

func (*checkpointActivation) NeedLeaderElection() bool { return false }

func (g *checkpointActivation) Start(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		drained, err := g.drained(ctx)
		if err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "check previous checkpoint writers", "error", err)
		}
		if err == nil && drained {
			close(g.ready)
			<-ctx.Done()
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (g *checkpointActivation) drained(ctx context.Context) (bool, error) {
	selector := client.MatchingLabels{controllerComponentLabel: "controller"}
	if g.podName != "" {
		var self corev1.Pod
		if err := g.reader.Get(ctx, types.NamespacedName{Namespace: g.namespace, Name: g.podName}, &self); err != nil {
			return false, err
		}
		if self.UID != g.podUID || g.podUID == "" {
			return false, fmt.Errorf("controller pod identity changed")
		}
		if self.Labels[controllerComponentLabel] != "controller" {
			return false, fmt.Errorf("controller pod lacks component identity")
		}
		// Never include version or pod-template-hash: those would hide the old
		// generation. These are the chart's stable Deployment selector labels.
		for _, key := range []string{"app.kubernetes.io/name", "app.kubernetes.io/instance", controllerComponentLabel} {
			if self.Labels[key] == "" {
				return false, fmt.Errorf("controller pod lacks selector label %s", key)
			}
			selector[key] = self.Labels[key]
		}
	}
	// A failed old Pod disappearing is insufficient while its ReplicaSet can
	// recreate it. Confirm scale-down before the final Pod read; status must
	// acknowledge that generation and report no remaining replicas.
	var replicas appsv1.ReplicaSetList
	if err := g.reader.List(ctx, &replicas, client.InNamespace(g.namespace), selector); err != nil {
		return false, err
	}
	for _, rs := range replicas.Items {
		if rs.Spec.Template.Annotations[checkpointClaimsAnnotation] == checkpointClaimsVersion {
			continue
		}
		if rs.Spec.Replicas == nil || *rs.Spec.Replicas != 0 || rs.Status.ObservedGeneration < rs.Generation || rs.Status.Replicas != 0 {
			return false, nil
		}
	}
	var pods corev1.PodList
	if err := g.reader.List(ctx, &pods, client.InNamespace(g.namespace), selector); err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		if pod.UID == g.podUID && g.podUID != "" {
			continue
		}
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		// Unready and terminating legacy pods can still issue writes. Only
		// process exit or a compatible protocol marker makes them safe.
		if pod.Annotations[checkpointClaimsAnnotation] != checkpointClaimsVersion {
			return false, nil
		}
	}
	return true, nil
}
