package agentgateway

import (
	"context"
	"errors"
	"fmt"

	agw "github.com/agentgateway/agentgateway/controller/api/v1alpha1/agentgateway"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

type reconciler struct {
	client client.Client
	scheme *runtime.Scheme
	config Config
	model  bool
}

func (r *reconciler) parent() client.Object {
	if r.model {
		return &agw.AgentgatewayModel{}
	}
	return &agw.AgentgatewayBackend{}
}

func (r *reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	parent := r.parent()
	if err := r.client.Get(ctx, request.NamespacedName, parent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !parent.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}
	if parent.GetAnnotations()[ConnectionAnnotation] != "true" {
		if err := r.prune(ctx, parent, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.clearStatus(ctx, parent)
	}
	var objects []client.Object
	var err error
	if r.model {
		var model *kagentv1alpha3.ModelConfig
		model, err = r.config.Model(parent.(*agw.AgentgatewayModel))
		if err == nil {
			objects = []client.Object{model}
		}
	} else {
		objects, err = r.config.MCP(parent.(*agw.AgentgatewayBackend))
	}
	if err == nil {
		err = r.credential(ctx, parent)
	}
	if err == nil {
		for _, object := range objects {
			if err = r.apply(ctx, parent, object); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = r.prune(ctx, parent, objects)
	}
	return ctrl.Result{}, errors.Join(err, r.status(ctx, parent, err))
}

func credentialRef(parent client.Object) *agw.LocalSecretKeyRef {
	switch p := parent.(type) {
	case *agw.AgentgatewayModel:
		if p.Spec.Policies != nil && p.Spec.Policies.Auth != nil {
			return p.Spec.Policies.Auth.SecretRef
		}
	case *agw.AgentgatewayBackend:
		if p.Spec.Policies != nil && p.Spec.Policies.Auth != nil {
			return p.Spec.Policies.Auth.SecretRef
		}
	}
	return nil
}

func (r *reconciler) credential(ctx context.Context, parent client.Object) error {
	ref := credentialRef(parent)
	if ref == nil {
		return nil
	}
	if ref.Group != "" || (ref.Kind != "" && ref.Kind != "Secret") {
		return fmt.Errorf("connection credential must reference a core Secret")
	}
	secret := &corev1.Secret{}
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: parent.GetNamespace(), Name: string(ref.Name)}, secret); err != nil {
		return fmt.Errorf("failed to read connection credential %s: %w", ref.Name, err)
	}
	if ref.Key != nil && len(secret.Data[*ref.Key]) == 0 {
		return fmt.Errorf("connection credential %s has no value for key %s", ref.Name, *ref.Key)
	}
	return nil
}

func (r *reconciler) children(parent client.Object) []client.Object {
	key := metav1.ObjectMeta{Name: parent.GetName(), Namespace: parent.GetNamespace()}
	if r.model {
		return []client.Object{&kagentv1alpha3.ModelConfig{ObjectMeta: key}}
	}
	routeKey, policyKey := key, key
	routeKey.Name = "mcp-" + parent.GetName()
	policyKey.Name = parent.GetName() + "-runtime-auth"
	return []client.Object{
		&kagentv1alpha3.RemoteMCPServer{ObjectMeta: key},
		&gatewayv1.HTTPRoute{ObjectMeta: routeKey},
		&agw.AgentgatewayPolicy{ObjectMeta: policyKey},
	}
}

func (r *reconciler) prune(ctx context.Context, parent client.Object, desired []client.Object) error {
	type identity struct {
		kind schema.GroupVersionKind
		key  client.ObjectKey
	}
	retained := make(map[identity]bool, len(desired))
	for _, object := range desired {
		kind, err := apiutil.GVKForObject(object, r.scheme)
		if err != nil {
			return err
		}
		retained[identity{kind: kind, key: client.ObjectKeyFromObject(object)}] = true
	}
	for _, object := range r.children(parent) {
		kind, err := apiutil.GVKForObject(object, r.scheme)
		if err != nil {
			return err
		}
		if retained[identity{kind: kind, key: client.ObjectKeyFromObject(object)}] {
			continue
		}
		if err := r.client.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if !metav1.IsControlledBy(object, parent) {
			continue
		}
		if err := r.client.Delete(ctx, object, client.Preconditions{UID: new(object.GetUID())}); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to remove connection consumer: %w", err)
		}
	}
	return nil
}
