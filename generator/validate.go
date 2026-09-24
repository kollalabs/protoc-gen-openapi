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
	validationRules := proto.GetExtension(field.Options(), validate.E_Rules)
	if validationRules == nil {
		return
	}
	fieldRules, ok := validationRules.(*validate.FieldRules)
	if !ok {
		return
	}
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
		if schema.Schema.Items == nil || len(schema.Schema.Items.SchemaOrReference) == 0 {
			return
		}
		// Message items are $refs, which can't carry per-field rules.
		itemSchema, ok := schema.Schema.Items.SchemaOrReference[0].Oneof.(*v3.SchemaOrReference_Schema)
		if !ok {
			return
		}
		fieldRule(fieldRules, field, itemSchema)
		return
	}

	fieldRule(fieldRules, field, schema)

}

func fieldRule(fieldRules *validate.FieldRules, field protoreflect.FieldDescriptor, schema *v3.SchemaOrReference_Schema) {

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
		if r := fieldRules.GetInt32(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Int64Kind:
		if r := fieldRules.GetInt64(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Sint32Kind:
		if r := fieldRules.GetSint32(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Sint64Kind:
		if r := fieldRules.GetSint64(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Sfixed32Kind:
		if r := fieldRules.GetSfixed32(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Sfixed64Kind:
		if r := fieldRules.GetSfixed64(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Uint32Kind:
		if r := fieldRules.GetUint32(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Uint64Kind:
		if r := fieldRules.GetUint64(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Fixed32Kind:
		if r := fieldRules.GetFixed32(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.Fixed64Kind:
		if r := fieldRules.GetFixed64(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.FloatKind:
		if r := fieldRules.GetFloat(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.DoubleKind:
		if r := fieldRules.GetDouble(); r != nil {
			applyNumericRules(schema.Schema, r.Const, r.Lt, r.Lte, r.Gt, r.Gte)
		}
	case protoreflect.EnumKind:
		enumRules := fieldRules.GetEnum()
		if enumRules == nil {
			break
		}

		// Rules reference enum numbers, not descriptor indexes.
		// we don't check enumRules.DefinedOnly because we already list the set of valid enums
		if enumRules.Const != nil {
			schema.Schema.Enum = enumNumbersToV3Any(field, func(n int32) bool { return n == enumRules.GetConst() })
		} else if len(enumRules.In) > 0 {
			schema.Schema.Enum = enumNumbersToV3Any(field, func(n int32) bool { return has(enumRules.In, n) })
		} else if len(enumRules.NotIn) > 0 {
			schema.Schema.Enum = enumNumbersToV3Any(field, func(n int32) bool { return !has(enumRules.NotIn, n) })
		}

	//TODO: implement protoc-gen-validate rules for the following types
	case protoreflect.BoolKind:

	case protoreflect.BytesKind:

	default:
		log.Printf("(TODO) Unsupported field type: %+v", fullMessageTypeName(field.Message()))
	}
}

type number interface {
	~int32 | ~int64 | ~uint32 | ~uint64 | ~float32 | ~float64
}

// applyNumericRules maps const, lt, lte, gt and gte rules onto the schema.
func applyNumericRules[T number](schema *v3.Schema, konst, lt, lte, gt, gte *T) {
	if konst != nil {
		setMinimum(schema, toFloat64(*konst), false)
		setMaximum(schema, toFloat64(*konst), false)
		return
	}

	var lower, upper *float64
	lowerExclusive, upperExclusive := false, false
	if gt != nil {
		v := toFloat64(*gt)
		lower, lowerExclusive = &v, true
	} else if gte != nil {
		v := toFloat64(*gte)
		lower = &v
	}
	if lt != nil {
		v := toFloat64(*lt)
		upper, upperExclusive = &v, true
	} else if lte != nil {
		v := toFloat64(*lte)
		upper = &v
	}

	// upper < lower means "outside the range", which minimum/maximum can't express.
	if lower != nil && upper != nil && *upper < *lower {
		return
	}
	if lower != nil {
		setMinimum(schema, *lower, lowerExclusive)
	}
	if upper != nil {
		setMaximum(schema, *upper, upperExclusive)
	}
}

// toFloat64 keeps float32 precision, so 0.1 stays 0.1.
func toFloat64[T number](v T) float64 {
	if f, ok := any(v).(float32); ok {
		parsed, _ := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
		return parsed
	}
	return float64(v)
}

func setMinimum(schema *v3.Schema, v float64, exclusive bool) {
	schema.Minimum = v
	schema.ExclusiveMinimum = exclusive
	if v == 0 {
		setZeroBound(schema, "minimum")
	}
}

func setMaximum(schema *v3.Schema, v float64, exclusive bool) {
	schema.Maximum = v
	schema.ExclusiveMaximum = exclusive
	if v == 0 {
		setZeroBound(schema, "maximum")
	}
}

// setZeroBound emits a 0 bound via an extension, since gnostic omits zero Minimum/Maximum.
func setZeroBound(schema *v3.Schema, key string) {
	schema.SpecificationExtension = append(schema.SpecificationExtension, &v3.NamedAny{
		Name:  key,
		Value: &v3.Any{Yaml: "0"},
	})
}
