package mcp

import (
	"strings"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// protoMessageSchema is the JSON schema of a message as protojson reads it,
// keyed by proto field names. Required fields and length bounds come from the
// message's protovalidate rules. Strings, bools, messages and lists of them
// are typed; any other field gets an open schema, which protojson and
// protovalidate still check on decode. descriptions names fields by their
// proto name; nested fields are named parent.child.
func protoMessageSchema(desc protoreflect.MessageDescriptor, descriptions map[string]string) *jsonschema.Schema {
	return messageSchema(desc, "", descriptions)
}

func messageSchema(desc protoreflect.MessageDescriptor, prefix string, descriptions map[string]string) *jsonschema.Schema {
	// Well-known types have their own JSON forms, and google.protobuf.Struct is
	// recursive.
	if strings.HasPrefix(string(desc.FullName()), "google.protobuf.") {
		return &jsonschema.Schema{}
	}
	schema := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}
	fields := desc.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		name := string(field.Name())
		path := prefix + name
		property := fieldSchema(field, path, descriptions)
		property.Description = descriptions[path]
		schema.Properties[name] = property
		schema.PropertyOrder = append(schema.PropertyOrder, name)
		if fieldRequired(field) {
			schema.Required = append(schema.Required, name)
		}
	}
	return schema
}

func fieldSchema(field protoreflect.FieldDescriptor, path string, descriptions map[string]string) *jsonschema.Schema {
	switch {
	case field.IsMap():
		return &jsonschema.Schema{}
	case field.IsList():
		schema := &jsonschema.Schema{Type: "array", Items: singularSchema(field, path, descriptions)}
		if limit := fieldRules(field).GetRepeated().GetMaxItems(); limit > 0 {
			schema.MaxItems = new(int(limit))
		}
		return schema
	}
	return singularSchema(field, path, descriptions)
}

func singularSchema(field protoreflect.FieldDescriptor, path string, descriptions map[string]string) *jsonschema.Schema {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return &jsonschema.Schema{Type: "boolean"}
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
	case protoreflect.MessageKind:
		return messageSchema(field.Message(), path+".", descriptions)
	}
	return &jsonschema.Schema{}
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
