package database

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// marshalRetainingUnknown keeps fields the upstream Go model cannot represent.
// Removed submessages stay removed. Repeated objects with IDs are matched by ID;
// ponytail: content parts have no IDs; match by position until upstream exposes stable identities.
func marshalRetainingUnknown(message proto.Message, previous []byte) ([]byte, error) {
	if len(previous) > 0 {
		stored := message.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(previous, stored); err != nil {
			return nil, fmt.Errorf("decode previous protobuf payload: %w", err)
		}
		retainUnknownFields(message.ProtoReflect(), stored.ProtoReflect())
	}
	return proto.Marshal(message)
}

func retainUnknownFields(next, previous protoreflect.Message) {
	next.SetUnknown(previous.GetUnknown())
	next.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Message() == nil || !previous.Has(field) {
			return true
		}
		old := previous.Get(field)
		switch {
		case field.IsMap():
			if field.MapValue().Message() != nil {
				value.Map().Range(func(key protoreflect.MapKey, entry protoreflect.Value) bool {
					if old.Map().Has(key) {
						retainUnknownFields(entry.Message(), old.Map().Get(key).Message())
					}
					return true
				})
			}
		case field.IsList():
			var identity protoreflect.FieldDescriptor
			for _, name := range []protoreflect.Name{"id", "message_id", "artifact_id"} {
				if id := field.Message().Fields().ByName(name); id != nil && id.Kind() == protoreflect.StringKind {
					identity = id
					break
				}
			}
			byID := map[string]protoreflect.Message{}
			if identity != nil {
				for i := 0; i < old.List().Len(); i++ {
					entry := old.List().Get(i).Message()
					byID[entry.Get(identity).String()] = entry
				}
			}
			for i := 0; i < value.List().Len(); i++ {
				entry := value.List().Get(i).Message()
				if identity != nil {
					if matched, ok := byID[entry.Get(identity).String()]; ok {
						retainUnknownFields(entry, matched)
					}
				} else if i < old.List().Len() {
					retainUnknownFields(entry, old.List().Get(i).Message())
				}
			}
		default:
			retainUnknownFields(value.Message(), old.Message())
		}
		return true
	})
}
