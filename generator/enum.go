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

func enumsToV3Any(field protoreflect.FieldDescriptor, enumValues ...int32) []*v3.Any {

	stringList := enumToStringSlice(field)
	list := []*v3.Any{}
	for _, v := range stringList {
		n := &v3.Any{
			Yaml: string(v),
		}
		list = append(list, n)
	}
	return list
}

func enumToStringSlice(field protoreflect.FieldDescriptor, enumValues ...int32) []string {
	list := []string{}
	values := field.Enum().Values()
	for i := 0; i < values.Len(); i++ {
		if len(enumValues) == 0 || has(enumValues, values.Get(i).Index()) {
			v := values.Get(i)
			// skip default unspecified values
			if strings.HasSuffix(string(v.Name()), "_UNSPECIFIED") {
				continue
			}
			list = append(list, string(v.Name()))
		}
	}

	return list
}

func has(list []int32, idx int) bool {
	for _, v := range list {
		if int(v) == idx {
			return true
		}
	}
	return false
}
