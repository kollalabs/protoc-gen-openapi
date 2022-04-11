// Copyright 2020 Google LLC. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package generator

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/envoyproxy/protoc-gen-validate/validate"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	v3 "github.com/google/gnostic/openapiv3"
)

type Configuration struct {
	Version     *string
	Title       *string
	Description *string
	Naming      *string
	Validate    *bool
}

const infoURL = "https://github.com/google/gnostic/tree/master/apps/protoc-gen-openapi"

// OpenAPIv3Generator holds internal state needed to generate an OpenAPIv3 document for a transcoded Protocol Buffer service.
type OpenAPIv3Generator struct {
	conf   Configuration
	plugin *protogen.Plugin

	requiredSchemas   []string // Names of schemas that need to be generated.
	generatedSchemas  []string // Names of schemas that have already been generated.
	linterRulePattern *regexp.Regexp
	pathPattern       *regexp.Regexp
	namedPathPattern  *regexp.Regexp
}

// NewOpenAPIv3Generator creates a new generator for a protoc plugin invocation.
func NewOpenAPIv3Generator(plugin *protogen.Plugin, conf Configuration) *OpenAPIv3Generator {
	return &OpenAPIv3Generator{
		conf:   conf,
		plugin: plugin,

		requiredSchemas:   make([]string, 0),
		generatedSchemas:  make([]string, 0),
		linterRulePattern: regexp.MustCompile(`\(-- .* --\)`),
		pathPattern:       regexp.MustCompile("{([^=}]+)}"),
		namedPathPattern:  regexp.MustCompile("{(.+)=(.+)}"),
	}
}

// Run runs the generator.
func (g *OpenAPIv3Generator) Run() error {
	d := g.buildDocumentV3()
	bytes, err := d.YAMLValue("Generated with protoc-gen-openapi\n" + infoURL)
	if err != nil {
		return fmt.Errorf("failed to marshal yaml: %s", err.Error())
	}
	outputFile := g.plugin.NewGeneratedFile("openapi.yaml", "")
	outputFile.Write(bytes)
	return nil
}

// buildDocumentV3 builds an OpenAPIv3 document for a plugin request.
func (g *OpenAPIv3Generator) buildDocumentV3() *v3.Document {
	d := &v3.Document{}

	d.Openapi = "3.0.3"
	d.Info = &v3.Info{
		Version:     *g.conf.Version,
		Title:       *g.conf.Title,
		Description: *g.conf.Description,
	}

	d.Paths = &v3.Paths{}
	d.Components = &v3.Components{
		Schemas: &v3.SchemasOrReferences{
			AdditionalProperties: []*v3.NamedSchemaOrReference{},
		},
	}

	for _, file := range g.plugin.Files {
		if file.Generate {
			g.addPathsToDocumentV3(d, file)
		}
	}

	// If there is only 1 service, then use it's title for the document,
	//  if the document is missing it.
	if len(d.Tags) == 1 {
		if d.Info.Title == "" && d.Tags[0].Name != "" {
			d.Info.Title = d.Tags[0].Name + " API"
		}
		if d.Info.Description == "" {
			d.Info.Description = d.Tags[0].Description
		}
		d.Tags[0].Description = ""
	}

	for len(g.requiredSchemas) > 0 {
		count := len(g.requiredSchemas)
		for _, file := range g.plugin.Files {
			g.addSchemasToDocumentV3(d, file.Messages)
		}
		g.requiredSchemas = g.requiredSchemas[count:len(g.requiredSchemas)]
	}

	// Sort the tags.
	{
		pairs := d.Tags
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].Name < pairs[j].Name
		})
		d.Tags = pairs
	}
	// Sort the paths.
	{
		pairs := d.Paths.Path
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].Name < pairs[j].Name
		})
		d.Paths.Path = pairs
	}
	// Sort the schemas.
	{
		pairs := d.Components.Schemas.AdditionalProperties
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].Name < pairs[j].Name
		})
		d.Components.Schemas.AdditionalProperties = pairs
	}
	return d
}

// filterCommentString removes line breaks and linter rules from comments.
func (g *OpenAPIv3Generator) filterCommentString(c protogen.Comments, removeNewLines bool) string {
	comment := string(c)
	split := strings.SplitN(comment, "|", 2)
	if len(split) >= 2 {
		comment = split[1]
	}
	if removeNewLines {
		comment = strings.Replace(comment, "\n", "", -1)
	}
	comment = g.linterRulePattern.ReplaceAllString(comment, "")
	return strings.TrimSpace(comment)
}

// filterCommentStringForSummary prepares comment (or method name if there is no comment) for summary value on methods
func (g *OpenAPIv3Generator) filterCommentStringForSummary(c protogen.Comments, goName string) string {
	comment := string(c)
	split := strings.Split(comment, "|")
	if len(split) >= 2 {
		comment = split[0]
	} else {
		comment = goName
	}
	comment = g.linterRulePattern.ReplaceAllString(comment, "")
	return strings.TrimSpace(comment)
}

// addPathsToDocumentV3 adds paths from a specified file descriptor.
func (g *OpenAPIv3Generator) addPathsToDocumentV3(d *v3.Document, file *protogen.File) {
	for _, service := range file.Services {
		annotationsCount := 0

		for _, method := range service.Methods {
			comment := g.filterCommentString(method.Comments.Leading, false)
			inputMessage := method.Input
			outputMessage := method.Output
			summary := g.filterCommentStringForSummary(method.Comments.Leading, method.GoName)
			operationID := service.GoName + "_" + method.GoName
			xt := annotations.E_Http
			extension := proto.GetExtension(method.Desc.Options(), xt)
			var path string
			var methodName string
			var body string
			if extension != nil && extension != xt.InterfaceOf(xt.Zero()) {
				annotationsCount++

				rule := extension.(*annotations.HttpRule)
				body = rule.Body
				switch pattern := rule.Pattern.(type) {
				case *annotations.HttpRule_Get:
					path = pattern.Get
					methodName = "GET"
				case *annotations.HttpRule_Post:
					path = pattern.Post
					methodName = "POST"
				case *annotations.HttpRule_Put:
					path = pattern.Put
					methodName = "PUT"
				case *annotations.HttpRule_Delete:
					path = pattern.Delete
					methodName = "DELETE"
				case *annotations.HttpRule_Patch:
					path = pattern.Patch
					methodName = "PATCH"
				case *annotations.HttpRule_Custom:
					path = "custom-unsupported"
				default:
					path = "unknown-unsupported"
				}
			}
			if methodName != "" {
				op, path2 := g.buildOperationV3(
					file, summary, operationID, service.GoName, comment, path, body, inputMessage, outputMessage)
				g.addOperationV3(d, op, path2, methodName)
			}
		}

		if annotationsCount > 0 {
			comment := g.filterCommentString(service.Comments.Leading, false)
			d.Tags = append(d.Tags, &v3.Tag{Name: service.GoName, Description: comment})
		}
	}
}

func (g *OpenAPIv3Generator) formatMessageRef(name string) string {
	if *g.conf.Naming == "proto" {
		return name
	}

	if len(name) > 1 {
		return strings.ToUpper(name[0:1]) + name[1:]
	}

	if len(name) == 1 {
		return strings.ToLower(name)
	}

	return name
}

func getMessageName(message protoreflect.MessageDescriptor) string {
	prefix := ""
	parent := message.Parent()
	if message != nil {
		if _, ok := parent.(protoreflect.MessageDescriptor); ok {
			prefix = string(parent.Name()) + "_" + prefix
		}
	}

	return prefix + string(message.Name())
}

func (g *OpenAPIv3Generator) formatMessageName(message *protogen.Message) string {
	name := getMessageName(message.Desc)

	if *g.conf.Naming == "proto" {
		return name
	}

	if len(name) > 0 {
		return strings.ToUpper(name[0:1]) + name[1:]
	}

	return name
}

func (g *OpenAPIv3Generator) formatFieldName(field *protogen.Field) string {
	if *g.conf.Naming == "proto" {
		return string(field.Desc.Name())
	}

	return field.Desc.JSONName()
}

func (g *OpenAPIv3Generator) findAndFormatFieldName(name string, inMessage *protogen.Message) string {
	for _, field := range inMessage.Fields {
		if string(field.Desc.Name()) == name {
			return g.formatFieldName(field)
		}
	}

	return name
}

// buildOperationV3 constructs an operation for a set of values.
func (g *OpenAPIv3Generator) buildOperationV3(
	file *protogen.File,
	summary string,
	operationID string,
	tagName string,
	description string,
	path string,
	bodyField string,
	inputMessage *protogen.Message,
	outputMessage *protogen.Message,
) (*v3.Operation, string) {
	// coveredParameters tracks the parameters that have been used in the body or path.
	coveredParameters := make([]string, 0)
	if bodyField != "" {
		coveredParameters = append(coveredParameters, bodyField)
	}
	// Initialize the list of operation parameters.
	parameters := []*v3.ParameterOrReference{}

	// Build a list of path parameters.
	pathParameters := make([]string, 0)
	// Find simple path parameters like {id}
	if allMatches := g.pathPattern.FindAllStringSubmatch(path, -1); allMatches != nil {
		for _, matches := range allMatches {
			// Add the value to the list of covered parameters.
			coveredParameters = append(coveredParameters, matches[1])
			pathParameter := g.findAndFormatFieldName(matches[1], inputMessage)
			path = strings.Replace(path, matches[1], pathParameter, 1)
			pathParameters = append(pathParameters, pathParameter)
		}
	}

	// Add the path parameters to the operation parameters.
	for _, pathParameter := range pathParameters {
		parameters = append(parameters,
			&v3.ParameterOrReference{
				Oneof: &v3.ParameterOrReference_Parameter{
					Parameter: &v3.Parameter{
						Name:     pathParameter,
						In:       "path",
						Required: true,
						Schema: &v3.SchemaOrReference{
							Oneof: &v3.SchemaOrReference_Schema{
								Schema: &v3.Schema{
									Type: "string",
								},
							},
						},
					},
				},
			})
	}

	// Build a list of named path parameters.
	namedPathParameters := make([]string, 0)
	// Find named path parameters like {name=shelves/*}
	if matches := g.namedPathPattern.FindStringSubmatch(path); matches != nil {
		// Add the "name=" "name" value to the list of covered parameters.
		coveredParameters = append(coveredParameters, matches[1])
		// Convert the path from the starred form to use named path parameters.
		starredPath := matches[2]
		parts := strings.Split(starredPath, "/")
		// The starred path is assumed to be in the form "things/*/otherthings/*".
		// We want to convert it to "things/{thingsId}/otherthings/{otherthingsId}".
		for i := 0; i < len(parts)-1; i += 2 {
			section := parts[i]
			namedPathParameter := g.findAndFormatFieldName(section, inputMessage)
			namedPathParameter = singular(namedPathParameter)
			parts[i+1] = "{" + namedPathParameter + "}"
			namedPathParameters = append(namedPathParameters, namedPathParameter)
		}
		// Rewrite the path to use the path parameters.
		newPath := strings.Join(parts, "/")
		path = strings.Replace(path, matches[0], newPath, 1)
	}

	// Add the named path parameters to the operation parameters.
	for _, namedPathParameter := range namedPathParameters {
		parameters = append(parameters,
			&v3.ParameterOrReference{
				Oneof: &v3.ParameterOrReference_Parameter{
					Parameter: &v3.Parameter{
						Name:        namedPathParameter,
						In:          "path",
						Required:    true,
						Description: "The " + namedPathParameter + " id.",
						Schema: &v3.SchemaOrReference{
							Oneof: &v3.SchemaOrReference_Schema{
								Schema: &v3.Schema{
									Type: "string",
								},
							},
						},
					},
				},
			})
	}
	// Add any unhandled fields in the request message as query parameters.
	if bodyField != "*" {
		for _, field := range inputMessage.Fields {
			fieldName := string(field.Desc.Name())
			if !contains(coveredParameters, fieldName) {
				bodyFieldName := g.formatFieldName(field)
				// Get the field description from the comments.
				fieldDescription := g.filterCommentString(field.Comments.Leading, true)

				schema := g.schemaOrReferenceForField(field.Desc)
				schema = coalesceToStringSchema(schema, "number", "integer")

				g.addValidationRules(schema, field.Desc)

				parameters = append(parameters,
					&v3.ParameterOrReference{
						Oneof: &v3.ParameterOrReference_Parameter{
							Parameter: &v3.Parameter{
								Name:        bodyFieldName,
								In:          "query",
								Description: fieldDescription,
								Required:    false,
								Schema:      schema,
							},
						},
					})
			}
		}
	}
	// Create the response.
	responses := &v3.Responses{
		ResponseOrReference: []*v3.NamedResponseOrReference{
			{
				Name: "200",
				Value: &v3.ResponseOrReference{
					Oneof: &v3.ResponseOrReference_Response{
						Response: &v3.Response{
							Description: "OK",
							Content:     g.responseContentForMessage(outputMessage),
						},
					},
				},
			},
		},
	}
	// Create the operation.
	op := &v3.Operation{
		Tags:        []string{tagName},
		Description: description,
		Summary:     summary,
		OperationId: operationID,
		Parameters:  parameters,
		Responses:   responses,
	}
	// If a body field is specified, we need to pass a message as the request body.
	if bodyField != "" {
		var bodyFieldScalarTypeName string
		var bodyFieldMessageTypeName string
		if bodyField == "*" {
			// Pass the entire request message as the request body.
			bodyFieldMessageTypeName = fullMessageTypeName(inputMessage.Desc)
		} else {
			// If body refers to a message field, use that type.
			for _, field := range inputMessage.Fields {
				if string(field.Desc.Name()) == bodyField {
					switch field.Desc.Kind() {
					case protoreflect.StringKind:
						bodyFieldScalarTypeName = "string"
					case protoreflect.MessageKind:
						bodyFieldMessageTypeName = fullMessageTypeName(field.Message.Desc)
					default:
						log.Printf("unsupported field type %+v", field.Desc)
					}
					break
				}
			}
		}
		var requestSchema *v3.SchemaOrReference
		if bodyFieldScalarTypeName != "" {
			requestSchema = &v3.SchemaOrReference{
				Oneof: &v3.SchemaOrReference_Schema{
					Schema: &v3.Schema{
						Type: bodyFieldScalarTypeName,
					},
				},
			}
		} else if bodyFieldMessageTypeName != "" {
			switch bodyFieldMessageTypeName {
			case ".google.protobuf.Empty":
				fallthrough
			case ".google.protobuf.Struct":
				requestSchema = &v3.SchemaOrReference{
					Oneof: &v3.SchemaOrReference_Schema{
						Schema: &v3.Schema{
							Type: "object",
						},
					},
				}
			default:
				requestSchema = &v3.SchemaOrReference{
					Oneof: &v3.SchemaOrReference_Reference{
						Reference: &v3.Reference{
							XRef: g.schemaReferenceForTypeName(bodyFieldMessageTypeName),
						}},
				}
			}
		}
		op.RequestBody = &v3.RequestBodyOrReference{
			Oneof: &v3.RequestBodyOrReference_RequestBody{
				RequestBody: &v3.RequestBody{
					Required: true,
					Content: &v3.MediaTypes{
						AdditionalProperties: []*v3.NamedMediaType{
							{
								Name: "application/json",
								Value: &v3.MediaType{
									Schema: requestSchema,
								},
							},
						},
					},
				},
			},
		}
	}
	return op, path
}

// addOperationV3 adds an operation to the specified path/method.
func (g *OpenAPIv3Generator) addOperationV3(d *v3.Document, op *v3.Operation, path string, methodName string) {
	var selectedPathItem *v3.NamedPathItem
	for _, namedPathItem := range d.Paths.Path {
		if namedPathItem.Name == path {
			selectedPathItem = namedPathItem
			break
		}
	}
	// If we get here, we need to create a path item.
	if selectedPathItem == nil {
		selectedPathItem = &v3.NamedPathItem{Name: path, Value: &v3.PathItem{}}
		d.Paths.Path = append(d.Paths.Path, selectedPathItem)
	}
	// Set the operation on the specified method.
	switch methodName {
	case "GET":
		selectedPathItem.Value.Get = op
	case "POST":
		selectedPathItem.Value.Post = op
	case "PUT":
		selectedPathItem.Value.Put = op
	case "DELETE":
		selectedPathItem.Value.Delete = op
	case "PATCH":
		selectedPathItem.Value.Patch = op
	}
}

// schemaReferenceForTypeName returns an OpenAPI JSON Reference to the schema that represents a type.
func (g *OpenAPIv3Generator) schemaReferenceForTypeName(typeName string) string {
	if !contains(g.requiredSchemas, typeName) {
		g.requiredSchemas = append(g.requiredSchemas, typeName)
	}
	parts := strings.Split(typeName, ".")
	lastPart := parts[len(parts)-1]
	return "#/components/schemas/" + g.formatMessageRef(lastPart)
}

// fullMessageTypeName builds the full type name of a message.
func fullMessageTypeName(message protoreflect.MessageDescriptor) string {
	name := getMessageName(message)
	return "." + string(message.ParentFile().Package()) + "." + name
}

func (g *OpenAPIv3Generator) responseContentForMessage(outputMessage *protogen.Message) *v3.MediaTypes {
	typeName := fullMessageTypeName(outputMessage.Desc)

	if typeName == ".google.protobuf.Empty" {
		return &v3.MediaTypes{}
	}
	if typeName == ".google.protobuf.Struct" {
		return &v3.MediaTypes{}
	}

	if typeName == ".google.api.HttpBody" {
		return &v3.MediaTypes{
			AdditionalProperties: []*v3.NamedMediaType{
				{
					Name:  "application/octet-stream",
					Value: &v3.MediaType{},
				},
			},
		}
	}

	return &v3.MediaTypes{
		AdditionalProperties: []*v3.NamedMediaType{
			{
				Name: "application/json",
				Value: &v3.MediaType{
					Schema: &v3.SchemaOrReference{
						Oneof: &v3.SchemaOrReference_Reference{
							Reference: &v3.Reference{
								XRef: g.schemaReferenceForTypeName(fullMessageTypeName(outputMessage.Desc)),
							},
						},
					},
				},
			},
		},
	}
}

func (g *OpenAPIv3Generator) schemaOrReferenceForType(typeName string) *v3.SchemaOrReference {

	switch typeName {

	case ".google.protobuf.Timestamp":
		// Timestamps are serialized as strings
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "string", Format: "date-time"}}}

	case ".google.type.Date":
		// Dates are serialized as strings
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "string", Format: "date"}}}

	case ".google.type.DateTime":
		// DateTimes are serialized as strings
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "string", Format: "date-time"}}}

	case ".google.protobuf.Struct":
		// Struct is equivalent to a JSON object
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "object"}}}

	case ".google.protobuf.Empty":
		// Empty is close to JSON undefined than null, so ignore this field
		return nil //&v3.SchemaOrReference{Oneof: &v3.SchemaOrReference_Schema{Schema: &v3.Schema{Type: "null"}}}

	case ".google.protobuf.BoolValue":
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "boolean", Nullable: true}}}

	case ".google.protobuf.BytesValue":
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "string", Format: "bytes", Nullable: true}}}

	case ".google.protobuf.DoubleValue", ".google.protobuf.FloatValue":
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "number", Nullable: true, Format: "float"}}} // TODO: put the correct format here

	case ".google.protobuf.Int64Value", ".google.protobuf.UInt64Value",
		".google.protobuf.Int32Value", ".google.protobuf.UInt32Value":
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "integer", Nullable: true}}} // TODO: put the correct format here

	case ".google.protobuf.StringValue":
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "string", Nullable: true}}}

	default:
		ref := g.schemaReferenceForTypeName(typeName)
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Reference{
				Reference: &v3.Reference{XRef: ref}}}
	}
}

func (g *OpenAPIv3Generator) schemaOrReferenceForField(field protoreflect.FieldDescriptor) *v3.SchemaOrReference {
	if field.IsMap() {
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "object",
					AdditionalProperties: &v3.AdditionalPropertiesItem{
						Oneof: &v3.AdditionalPropertiesItem_SchemaOrReference{
							SchemaOrReference: g.schemaOrReferenceForField(field.MapValue())}}}}}
	}

	var kindSchema *v3.SchemaOrReference

	kind := field.Kind()

	switch kind {

	case protoreflect.MessageKind:
		typeName := fullMessageTypeName(field.Message())
		kindSchema = g.schemaOrReferenceForType(typeName)
		if kindSchema == nil {
			return nil
		}

	case protoreflect.StringKind:

		kindSchema = &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "string"}}}

	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind, protoreflect.Sfixed64Kind,
		protoreflect.Fixed64Kind:
		kindSchema = &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "integer", Format: kind.String()}}}

	case protoreflect.EnumKind:
		kindSchema = g.enumKindSchema(field)

	case protoreflect.BoolKind:
		kindSchema = &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "boolean"}}}

	case protoreflect.FloatKind, protoreflect.DoubleKind:
		kindSchema = &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "number", Format: kind.String()}}}

	case protoreflect.BytesKind:
		kindSchema = &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{Type: "string", Format: "bytes"}}}

	default:
		log.Printf("(TODO) Unsupported field type: %+v", fullMessageTypeName(field.Message()))
	}

	if field.IsList() {
		return &v3.SchemaOrReference{
			Oneof: &v3.SchemaOrReference_Schema{
				Schema: &v3.Schema{
					Type:  "array",
					Items: &v3.ItemsItem{SchemaOrReference: []*v3.SchemaOrReference{kindSchema}},
				},
			},
		}
	}

	return kindSchema
}

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
		log.Printf("(TODO) Unsupported field type: %+v", fullMessageTypeName(field.Message()))
	}

	if field.IsList() {
		log.Printf("(TODO) Unsupported field type: list.")
		return
	}

}

// addSchemasToDocumentV3 adds info from one file descriptor.
func (g *OpenAPIv3Generator) addSchemasToDocumentV3(d *v3.Document, messages []*protogen.Message) {
	// For each message, generate a definition.
	for _, message := range messages {

		if message.Messages != nil {
			g.addSchemasToDocumentV3(d, message.Messages)
		}
		xt := annotations.E_Resource
		extension := proto.GetExtension(message.Desc.Options(), xt)
		pattern := ""

		if extension != nil && extension != xt.InterfaceOf(xt.Zero()) {
			rule := extension.(*annotations.ResourceDescriptor)
			if len(rule.Pattern) > 0 {
				pattern = rule.Pattern[0]
			}
		}

		typeName := fullMessageTypeName(message.Desc)
		log.Printf("Adding %s", typeName)

		// Only generate this if we need it and haven't already generated it.
		if !contains(g.requiredSchemas, typeName) ||
			contains(g.generatedSchemas, typeName) {
			continue
		}
		g.generatedSchemas = append(g.generatedSchemas, typeName)
		// Get the message description from the comments.
		messageDescription := g.filterCommentString(message.Comments.Leading, true)
		// Build an array holding the fields of the message.
		definitionProperties := &v3.Properties{
			AdditionalProperties: make([]*v3.NamedSchemaOrReference, 0),
		}
		var requiredProperites []string
		for _, field := range message.Fields {
			// Check the field annotations to see if this is a readonly field.
			outputOnly := false
			inputOnly := false
			required := false
			extension := proto.GetExtension(field.Desc.Options(), annotations.E_FieldBehavior)
			if extension != nil {
				switch v := extension.(type) {
				case []annotations.FieldBehavior:
					for _, vv := range v {
						if vv == annotations.FieldBehavior_OUTPUT_ONLY {
							outputOnly = true
						}
						if vv == annotations.FieldBehavior_REQUIRED {
							required = true
						}
						if vv == annotations.FieldBehavior_INPUT_ONLY {
							inputOnly = true
						}
					}
				default:
					log.Printf("unsupported extension type %T", extension)
				}
			}

			// The field is either described by a reference or a schema.
			fieldSchema := g.schemaOrReferenceForField(field.Desc)
			if fieldSchema == nil {
				continue
			}
			if schema, ok := fieldSchema.Oneof.(*v3.SchemaOrReference_Schema); ok {
				if field.Desc.Name() == "name" && pattern != "" {
					pathParamsRX := regexp.MustCompile(`{[a-z_A-Z0-9]*}`)
					rPattern := "^" + pathParamsRX.ReplaceAllString(pattern, "[a-z2-7]{26}") + "$"
					schema.Schema.Pattern = rPattern
				}
				// Get the field description from the comments.
				schema.Schema.Description = g.filterCommentString(field.Comments.Leading, true)
				if outputOnly {
					schema.Schema.ReadOnly = true
				}
				if inputOnly {
					schema.Schema.WriteOnly = true
				}
				if required {
					requiredProperites = append(requiredProperites, g.formatFieldName(field))
				}
			}
			log.Println("About to call validate")
			if *g.conf.Validate {
				g.addValidationRules(fieldSchema, field.Desc)
			}

			definitionProperties.AdditionalProperties = append(
				definitionProperties.AdditionalProperties,
				&v3.NamedSchemaOrReference{
					Name:  g.formatFieldName(field),
					Value: fieldSchema,
				},
			)
		}
		// Add the schema to the components.schema list.
		d.Components.Schemas.AdditionalProperties = append(d.Components.Schemas.AdditionalProperties,
			&v3.NamedSchemaOrReference{
				Name: g.formatMessageName(message),
				Value: &v3.SchemaOrReference{
					Oneof: &v3.SchemaOrReference_Schema{
						Schema: &v3.Schema{
							Description: messageDescription,
							Properties:  definitionProperties,
							Required:    requiredProperites,
						},
					},
				},
			},
		)
	}
}

func coalesceToStringSchema(schema *v3.SchemaOrReference, allowed ...string) *v3.SchemaOrReference {

	stringSchema := &v3.SchemaOrReference{
		Oneof: &v3.SchemaOrReference_Schema{
			Schema: &v3.Schema{
				Type: "string",
			},
		},
	}

	switch t := schema.GetOneof().(type) {
	case *v3.SchemaOrReference_Schema:
		for _, v := range allowed {
			if t.Schema.Type == v {
				return schema
			}
		}
	}
	return stringSchema

}
