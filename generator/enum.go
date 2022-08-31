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

	stringList := enumToStringSlice(field, enumValues...)
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
	removeUnspecified := len(enumValues) == 0
	list := []string{}
	values := field.Enum().Values()
	for i := 0; i < values.Len(); i++ {
		if len(enumValues) == 0 || has(enumValues, int32(values.Get(i).Index())) {
			v := values.Get(i)
			// skip default unspecified values
			if removeUnspecified && strings.HasSuffix(string(v.Name()), "_UNSPECIFIED") {
				continue
			}
			list = append(list, string(v.Name()))
		}
	}

	return list
}

// enumValues returns of list of enum ids for a given field
func enumValues(field protoreflect.FieldDescriptor, removeUnspecified bool) []int32 {
	values := field.Enum().Values()
	var list []int32
	for i := 0; i < values.Len(); i++ {
		v := values.Get(i)
		// skip default unspecified values
		if removeUnspecified && strings.HasSuffix(string(v.Name()), "_UNSPECIFIED") {
			continue
		}

		list = append(list, int32(values.Get(i).Index()))
	}
	return list
}

func remove(list []int32, idxs ...int32) []int32 {
	var filtered []int32
	for _, v := range idxs {
		if !has(list, v) {
			filtered = append(filtered, int32(v))
		}
	}
	return filtered
}

func has(list []int32, idx int32) bool {
	for _, v := range list {
		if v == idx {
			return true
		}
	}
	return false
}
