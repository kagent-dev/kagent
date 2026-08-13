package controller

import (
	"context"
	"fmt"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// collectionReader lets the compiler use ordinary typed reads while KRT tracks
// every object fetched by a transformation as a recomputation dependency.
type collectionReader struct {
	ctx         krt.HandlerContext
	collections *Collections
}

func (r collectionReader) Get(_ context.Context, key types.NamespacedName, object runtime.Object) error {
	switch target := object.(type) {
	case *kagentv1alpha3.ModelConfig:
		source, err := fetchObject(r.ctx, r.collections.ModelConfigs, key, kagentv1alpha3.GroupVersion.WithResource("modelconfigs").GroupResource())
		if err == nil {
			*target = *source.DeepCopy()
		}
		return err
	case *kagentv1alpha3.RemoteMCPServer:
		source, err := fetchObject(r.ctx, r.collections.RemoteMCPServers, key, kagentv1alpha3.GroupVersion.WithResource("remotemcpservers").GroupResource())
		if err == nil {
			*target = *source.DeepCopy()
		}
		return err
	case *corev1.ConfigMap:
		source, err := fetchObject(r.ctx, r.collections.ConfigMaps, key, schema.GroupResource{Resource: "configmaps"})
		if err == nil {
			*target = *source.DeepCopy()
		}
		return err
	case *corev1.Secret:
		source, err := fetchObject(r.ctx, r.collections.Secrets, key, schema.GroupResource{Resource: "secrets"})
		if err == nil {
			*target = *source.DeepCopy()
		}
		return err
	case *atev1alpha1.WorkerPool:
		source, err := fetchObject(r.ctx, r.collections.WorkerPools, key, atev1alpha1.GroupVersion.WithResource("workerpools").GroupResource())
		if err == nil {
			*target = *source.DeepCopy()
		}
		return err
	default:
		return fmt.Errorf("unsupported KRT read type %T", object)
	}
}

func fetchObject[T controllers.ComparableObject](ctx krt.HandlerContext, collection krt.Collection[T], key types.NamespacedName, resource schema.GroupResource) (T, error) {
	object := krt.FetchOne(ctx, collection, krt.FilterObjectName(key))
	if object == nil {
		var zero T
		return zero, apierrors.NewNotFound(resource, key.Name)
	}
	return *object, nil
}
