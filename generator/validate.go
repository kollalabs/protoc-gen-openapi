package generator

import (
	"log"

	"github.com/envoyproxy/protoc-gen-validate/validate"
	v3 "github.com/google/gnostic/openapiv3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func (g *OpenAPIv3Generator) addValidationRules(fieldSchema *v3.SchemaOrReference, field protoreflect.FieldDescriptor) {
	log.Println("Let's validate!")
	validationRules := proto.GetExtension(field.Options(), validate.E_Rules)
	if validationRules == nil {
		return
	}
	fieldRules, ok := validationRules.(*validate.FieldRules)
	if !ok {
		return
	}
	log.Println("Found some rules... let's go!")
	schema, ok := fieldSchema.Oneof.(*v3.SchemaOrReference_Schema)
	if !ok {
		return
	}
	//TODO: implement map validation
	if field.IsMap() {
		return
	}

	log.Println("Check kind of field")
	kind := field.Kind()

	switch kind {

	case protoreflect.MessageKind:
		return // TO DO: Implement message validators from protoc-gen-validate

	case protoreflect.StringKind:
		stringRules := fieldRules.GetString_()
		if stringRules != nil {
			// Set Format
			// format is an open value, so you can use any formats, even not those defined by the OpenAPI Specification
			if stringRules.GetEmail() {
				schema.Schema.Format = "email"
			} else if stringRules.GetHostname() {
				schema.Schema.Format = "hostname"
			} else if stringRules.GetIp() {
				schema.Schema.Format = "ip"
			} else if stringRules.GetIpv4() {
				schema.Schema.Format = "ipv4"
			} else if stringRules.GetIpv6() {
				schema.Schema.Format = "ipv6"
			} else if stringRules.GetUri() {
				schema.Schema.Format = "uri"
			} else if stringRules.GetUriRef() {
				schema.Schema.Format = "uri_ref"
			} else if stringRules.GetUuid() {
				schema.Schema.Format = "uuid"
			}
			// Set min/max
			if stringRules.GetMinLen() > 0 {
				schema.Schema.MinLength = int64(stringRules.GetMinLen())
			}
			if stringRules.GetMaxLen() > 0 {
				schema.Schema.MaxLength = int64(stringRules.GetMaxLen())
			}
			// Set Pattern
			if stringRules.GetPattern() != "" {
				schema.Schema.Pattern = stringRules.GetPattern()
			}

		}

	case protoreflect.Int32Kind:
		int32Rules := fieldRules.GetInt32()
		if int32Rules != nil {
			if int32Rules.GetGte() > 0 {
				schema.Schema.Minimum = float64(int32Rules.GetGte())
			}
			if int32Rules.GetLte() > 0 {
				schema.Schema.Maximum = float64(int32Rules.GetLte())
			}
		}
	case protoreflect.Int64Kind:
		int64Rules := fieldRules.GetInt64()
		if int64Rules != nil {
			if int64Rules.GetGte() > 0 {
				schema.Schema.Minimum = float64(int64Rules.GetGte())
			}
			if int64Rules.GetLte() > 0 {
				schema.Schema.Maximum = float64(int64Rules.GetLte())
			}
		}
	//TODO:
	case protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Sint64Kind, protoreflect.Uint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind, protoreflect.Sfixed64Kind,
		protoreflect.Fixed64Kind:

	case protoreflect.EnumKind:

	case protoreflect.BoolKind:

	case protoreflect.FloatKind, protoreflect.DoubleKind:

	case protoreflect.BytesKind:

	default:
		log.Printf("(TODO) Unsupported field type: %+v", g.reflect.fullMessageTypeName(field.Message()))
	}

	if field.IsList() {
		log.Printf("(TODO) Unsupported field type: list.")
		return
	}

}
