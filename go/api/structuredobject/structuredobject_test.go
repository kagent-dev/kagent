package structuredobject

import (
	"errors"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/types/known/structpb"
)

type testResource struct {
	Name      string    `json:"name"`
	Count     int64     `json:"count"`
	CreatedAt time.Time `json:"createdAt"`
}

func TestRoundTrip(t *testing.T) {
	want := testResource{
		Name:      "example",
		Count:     42,
		CreatedAt: time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC),
	}

	object, err := FromGo(want, "kagent.dev/v1alpha3", "Example", 16<<20)
	if err != nil {
		t.Fatalf("FromGo() error = %v", err)
	}
	if object.GetApiVersion() != "kagent.dev/v1alpha3" || object.GetKind() != "Example" {
		t.Fatalf("FromGo() identity = %q/%q", object.GetApiVersion(), object.GetKind())
	}

	var got testResource
	if err := ToGo(object, "Example", &got, 16<<20); err != nil {
		t.Fatalf("ToGo() error = %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestFromGoValidation(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		kind    string
		max     int
		wantErr error
	}{
		{name: "empty kind", value: map[string]any{}, wantErr: ErrEmptyKind},
		{name: "non object", value: []string{"value"}, kind: "List", wantErr: ErrNonObjectRoot},
		{name: "too large", value: map[string]string{"value": "large"}, kind: "Example", max: 1, wantErr: ErrValueTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := FromGo(test.value, "v1", test.kind, test.max)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("FromGo() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestToGoValidation(t *testing.T) {
	value, err := structpb.NewStruct(map[string]any{"known": "value", "extra": true})
	if err != nil {
		t.Fatal(err)
	}
	type destination struct {
		Known string `json:"known"`
	}

	tests := []struct {
		name    string
		object  *apiv1alpha1.StructuredObject
		kind    string
		max     int
		wantErr error
	}{
		{name: "nil object", wantErr: ErrNilValue},
		{name: "nil value", object: &apiv1alpha1.StructuredObject{Kind: "Example"}, wantErr: ErrNilValue},
		{name: "wrong kind", object: &apiv1alpha1.StructuredObject{Kind: "Other", Value: value}, kind: "Example", wantErr: ErrKindMismatch},
		{name: "too large", object: &apiv1alpha1.StructuredObject{Kind: "Example", Value: value}, kind: "Example", max: 1, wantErr: ErrValueTooLarge},
		{name: "unknown field", object: &apiv1alpha1.StructuredObject{Kind: "Example", Value: value}, kind: "Example"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got destination
			err := ToGo(test.object, test.kind, &got, test.max)
			if test.name == "unknown field" {
				if err == nil {
					t.Fatal("ToGo() error = nil, want unknown field error")
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ToGo() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
