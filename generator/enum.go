package generator

import (
	"strings"

	v3 "github.com/google/gnostic/openapiv3"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func (g *OpenAPIv3Generator) enumKindSchema(field protoreflect.FieldDescriptor) *v3.SchemaOrReference {

	list := []*v3.Any{}
	values := field.Enum().Values()
	for i := 0; i < values.Len(); i++ {
		v := values.Get(i)
		// skip default unspecified values
		if strings.HasSuffix(string(v.Name()), "_UNSPECIFIED") {
			continue
		}

		n := &v3.Any{
			Yaml: string(v.Name()),
		}
		list = append(list, n)
	}

	s := &v3.SchemaOrReference{
		Oneof: &v3.SchemaOrReference_Schema{
			Schema: &v3.Schema{
				Type: "string",
				Enum: list,
			},
		},
	}

	return s
}
