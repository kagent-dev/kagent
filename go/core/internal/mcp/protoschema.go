package mcp

import (
	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// maxProtoSchemaDepth bounds how deep a message schema nests, so a recursive
// message such as google.protobuf.Struct ends in an open schema.
const maxProtoSchemaDepth = 6

// protoMessageSchema is the JSON schema of a message as protojson reads it,
// keyed by proto field names. Required fields and string length bounds come
// from the message's protovalidate rules. descriptions names fields by their
// proto name; nested fields are named parent.child.
func protoMessageSchema(desc protoreflect.MessageDescriptor, descriptions map[string]string) *jsonschema.Schema {
	return messageSchema(desc, "", descriptions, 0)
}

func messageSchema(desc protoreflect.MessageDescriptor, prefix string, descriptions map[string]string, depth int) *jsonschema.Schema {
	if schema, ok := wellKnownSchema(desc); ok {
		return schema
	}
	schema := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}
	if depth >= maxProtoSchemaDepth {
		return schema
	}
	fields := desc.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		name := string(field.Name())
		path := prefix + name
		property := fieldSchema(field, path, descriptions, depth)
		property.Description = descriptions[path]
		schema.Properties[name] = property
		schema.PropertyOrder = append(schema.PropertyOrder, name)
		if fieldRequired(field) {
			schema.Required = append(schema.Required, name)
		}
	}
	return schema
}

func fieldSchema(field protoreflect.FieldDescriptor, path string, descriptions map[string]string, depth int) *jsonschema.Schema {
	switch {
	case field.IsMap():
		return &jsonschema.Schema{Type: "object", AdditionalProperties: singularSchema(field.MapValue(), path, descriptions, depth)}
	case field.IsList():
		items := singularSchema(field, path, descriptions, depth)
		schema := &jsonschema.Schema{Type: "array", Items: items}
		if limit := fieldRules(field).GetRepeated().GetMaxItems(); limit > 0 {
			schema.MaxItems = new(int(limit))
		}
		return schema
	}
	return singularSchema(field, path, descriptions, depth)
}

func singularSchema(field protoreflect.FieldDescriptor, path string, descriptions map[string]string, depth int) *jsonschema.Schema {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return &jsonschema.Schema{Type: "boolean"}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return &jsonschema.Schema{Type: "integer"}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		// protojson writes 64-bit integers as strings and reads either.
		return &jsonschema.Schema{Types: []string{"integer", "string"}}
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return &jsonschema.Schema{Type: "number"}
	case protoreflect.StringKind:
		schema := &jsonschema.Schema{Type: "string"}
		rules := fieldRules(field).GetString()
		if rules.HasMinLen() {
			schema.MinLength = new(int(rules.GetMinLen()))
		}
		if rules.HasMaxLen() {
			schema.MaxLength = new(int(rules.GetMaxLen()))
		}
		return schema
	case protoreflect.BytesKind:
		return &jsonschema.Schema{Type: "string", ContentEncoding: "base64"}
	case protoreflect.EnumKind:
		values := field.Enum().Values()
		schema := &jsonschema.Schema{Type: "string"}
		for i := range values.Len() {
			schema.Enum = append(schema.Enum, string(values.Get(i).Name()))
		}
		return schema
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return messageSchema(field.Message(), path+".", descriptions, depth+1)
	}
	return &jsonschema.Schema{}
}

func wellKnownSchema(desc protoreflect.MessageDescriptor) (*jsonschema.Schema, bool) {
	switch desc.FullName() {
	case "google.protobuf.Timestamp":
		return &jsonschema.Schema{Type: "string", Format: "date-time"}, true
	case "google.protobuf.Duration":
		return &jsonschema.Schema{Type: "string", Description: "Seconds with an s suffix, such as 3600s"}, true
	case "google.protobuf.Struct":
		return &jsonschema.Schema{Type: "object"}, true
	case "google.protobuf.Value":
		return &jsonschema.Schema{}, true
	case "google.protobuf.FieldMask":
		return &jsonschema.Schema{Type: "string"}, true
	case "google.protobuf.BoolValue":
		return &jsonschema.Schema{Type: "boolean"}, true
	case "google.protobuf.StringValue", "google.protobuf.BytesValue":
		return &jsonschema.Schema{Type: "string"}, true
	case "google.protobuf.Int32Value", "google.protobuf.UInt32Value":
		return &jsonschema.Schema{Type: "integer"}, true
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return &jsonschema.Schema{Types: []string{"integer", "string"}}, true
	case "google.protobuf.FloatValue", "google.protobuf.DoubleValue":
		return &jsonschema.Schema{Type: "number"}, true
	}
	return nil, false
}

func fieldRules(field protoreflect.FieldDescriptor) *validate.FieldRules {
	rules, _ := proto.GetExtension(field.Options(), validate.E_Field).(*validate.FieldRules)
	return rules
}

// fieldRequired says whether protovalidate refuses the field unset: it is
// marked required, or it is a singular string with a minimum length.
func fieldRequired(field protoreflect.FieldDescriptor) bool {
	rules := fieldRules(field)
	if rules.GetRequired() {
		return true
	}
	return field.Kind() == protoreflect.StringKind && !field.IsList() && rules.GetString().GetMinLen() > 0
}
