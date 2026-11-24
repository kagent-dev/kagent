/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package modelproviderconfig

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

const (
	refreshInterval          = 5 * time.Minute
	reasonDiscoverySucceeded = "DiscoverySucceeded"
	reasonDiscoveryFailed    = "DiscoveryFailed"
)

// Reconciler publishes ModelProviderConfig discovery results to its status.
type Reconciler struct {
	client     client.Client //nolint:forbidigo // TODO(#3235): migrate to apiclient.Client
	discoverer *Discoverer
}

func New(client client.Client, discoverer *Discoverer) *Reconciler { //nolint:forbidigo // TODO(#3235): migrate to apiclient.Client
	return &Reconciler{client: client, discoverer: discoverer}
}

func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).
		For(&v1alpha3.ModelProviderConfig{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.requestsForSecret)).
		Complete(r)
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	config := &v1alpha3.ModelProviderConfig{}
	if err := r.client.Get(ctx, request.NamespacedName, config); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	models, err := r.discoverer.discover(ctx, config)
	if err != nil {
		if statusErr := r.updateStatus(ctx, config, 0, metav1.ConditionFalse, reasonDiscoveryFailed, err.Error()); statusErr != nil {
			statusErr = fmt.Errorf("update ModelProviderConfig discovery failure: %w", statusErr)
			return reconcile.Result{}, errors.Join(fmt.Errorf("discover ModelProviderConfig models: %w", err), statusErr)
		}
		return reconcile.Result{}, fmt.Errorf("discover ModelProviderConfig models: %w", err)
	}
	message := fmt.Sprintf("Discovered %d models", len(models))
	if err := r.updateStatus(ctx, config, len(models), metav1.ConditionTrue, reasonDiscoverySucceeded, message); err != nil {
		return reconcile.Result{}, fmt.Errorf("update ModelProviderConfig discovery status: %w", err)
	}
	return reconcile.Result{RequeueAfter: refreshInterval}, nil
}

func (r *Reconciler) updateStatus(
	ctx context.Context,
	config *v1alpha3.ModelProviderConfig,
	modelCount int,
	conditionStatus metav1.ConditionStatus,
	reason string,
	message string,
) error {
	original := config.DeepCopy()
	config.Status.ObservedGeneration = config.Generation
	config.Status.ModelCount = modelCount
	apiMeta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
		Type: v1alpha3.ModelProviderConfigConditionTypeReady, Status: conditionStatus, Reason: reason, Message: message,
		ObservedGeneration: config.Generation,
	})
	if reflect.DeepEqual(original.Status, config.Status) {
		return nil
	}
	return r.client.Status().Patch(ctx, config, client.MergeFrom(original))
}

func (r *Reconciler) requestsForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	configs := &v1alpha3.ModelProviderConfigList{}
	if err := r.client.List(ctx, configs, client.InNamespace(secret.GetNamespace())); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for i := range configs.Items {
		config := &configs.Items[i]
		if auth := config.Spec.DiscoveryAuth; auth != nil && auth.SecretRef.Name == secret.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: config.Namespace, Name: config.Name,
			}})
		}
	}
	return requests
}

var _ reconcile.Reconciler = (*Reconciler)(nil)
