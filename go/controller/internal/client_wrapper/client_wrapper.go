package client_wrapper

import (
	"context"
	"encoding/json"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type KubeClientWrapper interface {
	client.Client
	AddInMemory(obj client.Object) error
}

type typedObjectKey struct {
	gvk       schema.GroupVersionKind
	name      string
	namespace string
}

type kubeClientWrapper struct {
	client.Client
	inMemoryLock sync.RWMutex
	inMemory     map[typedObjectKey]client.Object
}

func NewKubeClientWrapper(kube client.Client) KubeClientWrapper {
	return &kubeClientWrapper{
		Client:   kube,
		inMemory: make(map[typedObjectKey]client.Object),
	}
}

func (w *kubeClientWrapper) AddInMemory(obj client.Object) error {
	w.inMemoryLock.Lock()
	defer w.inMemoryLock.Unlock()

	objKey, err := makeTypedObjectKey(w.Scheme(), obj, obj.GetName(), obj.GetNamespace())
	if err != nil {
		return err
	}

	w.inMemory[objKey] = obj

	return nil
}

func (w *kubeClientWrapper) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {

	exists, err := w.getInMemory(key, obj)
	if exists && err == nil {
		return nil
	}

	return w.Client.Get(ctx, key, obj, opts...)
}

func (w *kubeClientWrapper) getInMemory(
	key client.ObjectKey,
	obj client.Object,
) (bool, error) {
	w.inMemoryLock.RLock()
	defer w.inMemoryLock.RUnlock()

	objKey, err := makeTypedObjectKey(w.Scheme(), obj, key.Name, key.Namespace)
	if err != nil {
		return false, err
	}
	cachedObj, exists := w.inMemory[objKey]
	if !exists {
		return false, nil
	}

	cachedObjJson, err := json.Marshal(cachedObj)
	if err != nil {
		return false, err
	}

	err = json.Unmarshal(cachedObjJson, obj)
	if err != nil {
		return false, err
	}

	return true, nil
}

func makeTypedObjectKey(
	scheme *runtime.Scheme,
	obj client.Object,
	objName string,
	objNamespace string,
) (typedObjectKey, error) {
	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		return typedObjectKey{}, err
	}
	return typedObjectKey{
		gvk:       gvk,
		name:      objName,
		namespace: objNamespace,
	}, nil
}
