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

func TestNamespaceCache(t *testing.T) {
	if got := namespaceCache(nil); got != nil {
		t.Fatalf("namespaceCache(nil) = %#v, want nil", got)
	}
	got := namespaceCache([]string{"team-a", "team-b"})
	if len(got) != 2 {
		t.Fatalf("namespaceCache() = %#v", got)
	}
	if _, ok := got["team-a"]; !ok {
		t.Fatal("namespaceCache() missing team-a")
	}
	if _, ok := got["team-b"]; !ok {
		t.Fatal("namespaceCache() missing team-b")
	}
}
