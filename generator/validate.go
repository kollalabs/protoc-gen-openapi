package generator

import (
	"log"
	"strconv"

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

	if field.IsList() {
		repeatedRules := fieldRules.GetRepeated()
		if repeatedRules == nil {
			// no rules
			return
		}
		// MinItems specifies that this field must have the specified number of
		// items at a minimum
		// MaxItems specifies that this field must have the specified number of
		// items at a maximum
		// Unique specifies that all elements in this field must be unique. This
		// contraint is only applicable to scalar and enum types (messages are not
		// supported).
		// Items specifies the contraints to be applied to each item in the field.
		// Repeated message fields will still execute validation against each item
		// unless skip is specified here.
		// IgnoreEmpty specifies that the validation rules of this field should be
		// evaluated only if the field is not empty
		if repeatedRules.MinItems != nil {
			schema.Schema.MinItems = int64(*repeatedRules.MinItems)
		}
		if repeatedRules.MaxItems != nil {
			schema.Schema.MaxItems = int64(*repeatedRules.MaxItems)
		}

		// pull out the array items field rules
		fieldRules := repeatedRules.Items
		if fieldRules == nil {
			// no item specific rules
			return
		}
		schema := schema.Schema.Items.SchemaOrReference[0]
		fieldRule(fieldRules, field, schema.Oneof.(*v3.SchemaOrReference_Schema))

		log.Printf("(TODO) Unsupported field type: list.")
		return
	}

	fieldRule(fieldRules, field, schema)

}

func fieldRule(fieldRules *validate.FieldRules, field protoreflect.FieldDescriptor, schema *v3.SchemaOrReference_Schema) {

	log.Println("Check kind of field")
	kind := field.Kind()
	switch kind {

	case protoreflect.MessageKind:
		return // TO DO: Implement message validators from protoc-gen-validate

	case protoreflect.StringKind:
		stringRules := fieldRules.GetString_()
		if stringRules == nil {
			break
		}
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

	case protoreflect.Int32Kind:
		int32Rules := fieldRules.GetInt32()
		if int32Rules == nil {
			break
		}
		if int32Rules.GetGte() > 0 {
			schema.Schema.Minimum = float64(int32Rules.GetGte())
		}
		if int32Rules.GetLte() > 0 {
			schema.Schema.Maximum = float64(int32Rules.GetLte())
		}

	case protoreflect.Int64Kind:
		int64Rules := fieldRules.GetInt64()
		if int64Rules == nil {
			break
		}
		if int64Rules.GetGte() > 0 {
			schema.Schema.Minimum = float64(int64Rules.GetGte())
		}
		if int64Rules.GetLte() > 0 {
			schema.Schema.Maximum = float64(int64Rules.GetLte())
		}
	case protoreflect.EnumKind:
		enumRules := fieldRules.GetEnum()
		if enumRules == nil {
			break
		}
		if enumRules.Const != nil {
			schema.Schema.Enum = []*v3.Any{
				{
					Yaml: strconv.Itoa(int(*enumRules.Const)),
				},
			}
		} else if enumRules.NotIn != nil {

			schema.Schema.Enum = []*v3.Any{
				{
					Yaml: strconv.Itoa(int(*enumRules.Const)),
				},
			}
		}
	//TODO:
	case protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Sint64Kind, protoreflect.Uint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind, protoreflect.Sfixed64Kind,
		protoreflect.Fixed64Kind:

	case protoreflect.BoolKind:

	case protoreflect.FloatKind, protoreflect.DoubleKind:

	case protoreflect.BytesKind:

	default:
		log.Printf("(TODO) Unsupported field type: %+v", fullMessageTypeName(field.Message()))
	}
}
