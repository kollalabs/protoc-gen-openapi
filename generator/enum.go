package generator

import (
	"strings"

	v3 "github.com/google/gnostic/openapiv3"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func enumKindSchema(field protoreflect.FieldDescriptor) *v3.SchemaOrReference {
	list := enumsToV3Any(field)

	s := &v3.SchemaOrReference{
		Oneof: &v3.SchemaOrReference_Schema{
			Schema: &v3.Schema{
				Format: "enum",
				Type:   "string",
				Enum:   list,
			},
		},
	}

	return s
}

func enumsToV3Any(field protoreflect.FieldDescriptor) []*v3.Any {
	// skip default unspecified values
	return enumNamesToV3Any(field, func(v protoreflect.EnumValueDescriptor) bool {
		return !strings.HasSuffix(string(v.Name()), "_UNSPECIFIED")
	})
}

// enumNumbersToV3Any returns the names of the enum values whose numbers pass keep
func enumNumbersToV3Any(field protoreflect.FieldDescriptor, keep func(number int32) bool) []*v3.Any {
	return enumNamesToV3Any(field, func(v protoreflect.EnumValueDescriptor) bool {
		return keep(int32(v.Number()))
	})
}

func enumNamesToV3Any(field protoreflect.FieldDescriptor, keep func(v protoreflect.EnumValueDescriptor) bool) []*v3.Any {
	list := []*v3.Any{}
	values := field.Enum().Values()
	for i := 0; i < values.Len(); i++ {
		v := values.Get(i)
		if keep(v) {
			list = append(list, &v3.Any{Yaml: string(v.Name())})
		}
	}
	return list
}
