package utils

import (
	protoV2 "google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
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
func mkPointer(val reflect.Value) interface{} {
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
func DeepEqual(val1, val2 interface{}) bool {
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
