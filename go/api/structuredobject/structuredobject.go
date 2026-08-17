package structuredobject

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	ErrEmptyKind     = errors.New("structured object kind is empty")
	ErrKindMismatch  = errors.New("structured object kind does not match expected kind")
	ErrNilValue      = errors.New("structured object value is nil")
	ErrNonObjectRoot = errors.New("structured object JSON root is not an object")
	ErrValueTooLarge = errors.New("structured object exceeds configured size limit")
)

func FromGo(value any, apiVersion, kind string, maxBytes int) (*apiv1alpha1.StructuredObject, error) {
	if kind == "" {
		return nil, ErrEmptyKind
	}

	jsonValue, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal structured object: %w", err)
	}
	if maxBytes > 0 && len(jsonValue) > maxBytes {
		return nil, fmt.Errorf("%w: got %d bytes, limit %d", ErrValueTooLarge, len(jsonValue), maxBytes)
	}

	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(jsonValue))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode structured object: %w", err)
	}
	object, ok := decoded.(map[string]any)
	if !ok || object == nil {
		return nil, ErrNonObjectRoot
	}

	valueStruct, err := structpb.NewStruct(object)
	if err != nil {
		return nil, fmt.Errorf("create protobuf struct: %w", err)
	}
	return &apiv1alpha1.StructuredObject{
		ApiVersion: apiVersion,
		Kind:       kind,
		Value:      valueStruct,
	}, nil
}

func ToGo(object *apiv1alpha1.StructuredObject, expectedKind string, destination any, maxBytes int) error {
	if object == nil || object.GetValue() == nil {
		return ErrNilValue
	}
	if expectedKind != "" && object.GetKind() != expectedKind {
		return fmt.Errorf("%w: got %q, want %q", ErrKindMismatch, object.GetKind(), expectedKind)
	}

	jsonValue, err := object.GetValue().MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal protobuf struct: %w", err)
	}
	if maxBytes > 0 && len(jsonValue) > maxBytes {
		return fmt.Errorf("%w: got %d bytes, limit %d", ErrValueTooLarge, len(jsonValue), maxBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(jsonValue))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode structured object into destination: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return err
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode structured object: trailing JSON value")
		}
		return fmt.Errorf("decode structured object trailing data: %w", err)
	}
	return nil
}
