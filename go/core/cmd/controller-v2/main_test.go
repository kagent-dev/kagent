package main

import (
	"reflect"
	"testing"
)

func TestNamespaces(t *testing.T) {
	want := []string{"one", "two"}
	if got := namespaces(" one, ,two,"); !reflect.DeepEqual(got, want) {
		t.Fatalf("namespaces() = %q, want %q", got, want)
	}
}
