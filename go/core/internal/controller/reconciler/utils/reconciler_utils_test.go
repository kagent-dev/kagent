package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObjectMetasEqual_SameNamespaceNameLabelsAnnotations(t *testing.T) {
	a := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Namespace: "ns", Name: "n",
		Labels:      map[string]string{"a": "1"},
		Annotations: map[string]string{"b": "2"},
	}}
	b := a.DeepCopy()
	assert.True(t, ObjectMetasEqual(a, b))
}

func TestObjectMetasEqual_DifferentName(t *testing.T) {
	a := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "n1"}}
	b := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "n2"}}
	assert.False(t, ObjectMetasEqual(a, b))
}

func TestObjectMetasEqual_DifferentLabels(t *testing.T) {
	a := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}}}
	b := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "2"}}}
	assert.False(t, ObjectMetasEqual(a, b))
}

func TestObjectsEqual_DifferentData(t *testing.T) {
	a := &corev1.ConfigMap{Data: map[string]string{"k": "v1"}}
	b := &corev1.ConfigMap{Data: map[string]string{"k": "v2"}}
	assert.False(t, ObjectsEqual(a, b))
}

func TestObjectsEqual_SameDataEqual(t *testing.T) {
	a := &corev1.ConfigMap{Data: map[string]string{"k": "v1"}}
	b := &corev1.ConfigMap{Data: map[string]string{"k": "v1"}}
	assert.True(t, ObjectsEqual(a, b))
}

func TestObjectsEqual_DifferentTypesNotEqual(t *testing.T) {
	a := &corev1.ConfigMap{}
	b := &corev1.Secret{}
	assert.False(t, ObjectsEqual(a, b))
}

func TestMapStringEqual(t *testing.T) {
	assert.True(t, mapStringEqual(nil, nil))
	assert.True(t, mapStringEqual(map[string]string{}, nil))
	assert.True(t, mapStringEqual(map[string]string{"a": "1"}, map[string]string{"a": "1"}))
	assert.False(t, mapStringEqual(map[string]string{"a": "1"}, map[string]string{"a": "2"}))
	assert.False(t, mapStringEqual(map[string]string{"a": "1"}, map[string]string{"b": "1"}))
	assert.False(t, mapStringEqual(map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1"}))
}

func TestDeepEqual_NonProtoFallsBackToReflect(t *testing.T) {
	assert.True(t, DeepEqual(map[string]string{"a": "1"}, map[string]string{"a": "1"}))
	assert.False(t, DeepEqual(map[string]string{"a": "1"}, map[string]string{"a": "2"}))
}
