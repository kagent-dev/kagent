package utils

import (
	"context"
	"fmt"
	"maps"
	"reflect"

	protoV2 "google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var (
	ownerIndexKey = ".metadata.owner"
)

// returns true if "relevant" parts of obj1 and obj2 have equal:
// - labels,
// - annotations,
// - namespace+name,
// - non-metadata, non-status fields
// Note that Status fields are not compared.
// To compare status fields, use ObjectStatusesEqual
func ObjectsEqual(obj1, obj2 runtime.Object) bool {
	value1, value2 := reflect.ValueOf(obj1), reflect.ValueOf(obj2)

	if value1.Type() != value2.Type() {
		return false
	}

	if value1.Kind() == reflect.Ptr {
		value1 = value1.Elem()
		value2 = value2.Elem()
	}

	if meta1, hasMeta := obj1.(metav1.Object); hasMeta {
		if !ObjectMetasEqual(meta1, obj2.(metav1.Object)) {
			return false
		}
	}

	// recurse through fields of both, comparing each:
	for i := 0; i < value1.NumField(); i++ {
		field1Name := value1.Type().Field(i).Name
		if field1Name == "ObjectMeta" {
			// skip ObjectMeta field, as we already asserted relevant fields are equal
			continue
		}
		if field1Name == "TypeMeta" {
			// skip TypeMeta field, as it is set by the server and not relevant for object comparison
			continue
		}
		if field1Name == "Status" {
			// skip Status field, as it is considered a separate and not relevant for object comparison
			continue
		}

		field1 := mkPointer(value1.Field(i))
		field2 := mkPointer(value2.Field(i))

		// assert DeepEquality any other fields
		if !DeepEqual(field1, field2) {
			return false
		}
	}

	return true
}

// returns true if "relevant" parts of obj1 and obj2 have equal:
// -labels
// -annotations
// -namespace+name
// or if the objects are not metav1.Objects
func ObjectMetasEqual(obj1, obj2 metav1.Object) bool {
	return obj1.GetNamespace() == obj2.GetNamespace() &&
		obj1.GetName() == obj2.GetName() &&
		mapStringEqual(obj1.GetLabels(), obj2.GetLabels()) &&
		mapStringEqual(obj1.GetAnnotations(), obj2.GetAnnotations())
}

func mapStringEqual(map1, map2 map[string]string) bool {
	if map1 == nil && map2 == nil {
		return true
	}

	if len(map1) != len(map2) {
		return false
	}

	for key1, val1 := range map1 {
		val2, ok := map2[key1]
		if !ok {
			return false
		}
		if val1 != val2 {
			return false
		}
	}
	return true
}

// if i is a pointer, just return the value.
// if i is addressable, return that.
// if i is a struct passed in by value, make a new instance of the type and copy the contents to that and return
// the pointer to that.
func mkPointer(val reflect.Value) any {
	if val.Kind() == reflect.Ptr {
		return val.Interface()
	}
	if val.CanAddr() {
		return val.Addr().Interface()
	}
	if val.Kind() == reflect.Struct {
		nv := reflect.New(val.Type())
		nv.Elem().Set(val)
		return nv.Interface()
	}
	return val.Interface()
}

// DeepEqual should be used in place of reflect.DeepEqual when the type of an object is unknown and may be a proto message.
// see https://github.com/golang/protobuf/issues/1173 for details on why reflect.DeepEqual no longer works for proto messages
func DeepEqual(val1, val2 any) bool {
	protoVal1, isProto := val1.(protoV2.Message)
	if isProto {
		protoVal2, isProto := val2.(protoV2.Message)
		if !isProto {
			return false // different types
		}
		return protoV2.Equal(protoVal1, protoVal2)
	}
	return reflect.DeepEqual(val1, val2)
}

// SetupOwnerIndexes sets up caching and indexing of owned resources.
func SetupOwnerIndexes(mgr ctrl.Manager, ownedTypes []client.Object) error {
	for _, resource := range ownedTypes {
		if err := mgr.GetFieldIndexer().IndexField(context.Background(), resource, ownerIndexKey, func(rawObj client.Object) []string {
			owner := metav1.GetControllerOf(rawObj)
			if owner == nil {
				return nil
			}

			// This is an optimisation to avoid indexing every owned object,
			// only those owned by an Agent will be indexed. It may need to be
			// adjusted in future if other controllers start owning resources.
			if owner.Kind != "Agent" {
				return nil
			}

			return []string{string(owner.UID)}
		}); err != nil {
			return err
		}
	}

	return nil
}

// FindOwnedObjects looks for objects in the given namespace that have an owner
// reference. Note: this method assumes that an index has been setup for the
// owner reference using `SetupOwnerIndexes`.
func FindOwnedObjects(ctx context.Context, cl client.Client, uid types.UID, namespace string, objectTypes []client.Object) (map[types.UID]client.Object, error) {
	ownedObjects := map[types.UID]client.Object{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingFields{ownerIndexKey: string(uid)},
	}

	for _, objectType := range objectTypes {
		objs, err := GetList(ctx, cl, objectType, listOpts...)
		if err != nil {
			return nil, err
		}
		maps.Copy(ownedObjects, objs)
	}

	return ownedObjects, nil
}

// GetList queries the Kubernetes API to list the requested resource, setting
// the list l of type T.
func GetList[T client.Object](ctx context.Context, cl client.Client, l T, options ...client.ListOption) (map[types.UID]client.Object, error) {
	ownedObjects := map[types.UID]client.Object{}
	gvk, err := apiutil.GVKForObject(l, cl.Scheme())
	if err != nil {
		return nil, err
	}
	gvk.Kind = fmt.Sprintf("%sList", gvk.Kind)
	list, err := cl.Scheme().New(gvk)
	if err != nil {
		return nil, fmt.Errorf("unable to list objects of type %s: %w", gvk.Kind, err)
	}

	objList := list.(client.ObjectList)

	err = cl.List(ctx, objList, options...)
	if err != nil {
		return ownedObjects, fmt.Errorf("error listing %T: %w", l, err)
	}
	objs, err := meta.ExtractList(objList)
	if err != nil {
		return ownedObjects, fmt.Errorf("error listing %T: %w", l, err)
	}
	for i := range objs {
		typedObj, ok := objs[i].(T)
		if !ok {
			return ownedObjects, fmt.Errorf("failed to assert object at index %d to type %T", i, l)
		}
		ownedObjects[typedObj.GetUID()] = typedObj
	}
	return ownedObjects, nil
}

func UpsertOutput(ctx context.Context, kube client.Client, output client.Object) error {
	existing := output.DeepCopyObject().(client.Object)
	if err := kube.Get(ctx, client.ObjectKeyFromObject(existing), existing); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		return kube.Create(ctx, output)
	}
	output.SetResourceVersion(existing.GetResourceVersion())
	if err := kube.Update(ctx, output); err != nil {
		return err
	}
	return nil
}
