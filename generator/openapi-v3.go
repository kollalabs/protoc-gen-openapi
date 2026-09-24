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
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	status_pb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	any_pb "google.golang.org/protobuf/types/known/anypb"

	"github.com/golang/protobuf/ptypes/wrappers"
	wk "github.com/google/gnostic/cmd/protoc-gen-openapi/generator/wellknown"
	v3 "github.com/google/gnostic/openapiv3"

	open_api_extensions "github.com/kollalabs/protoc-gen-openapi/openapi"
)

type Configuration struct {
	Version           *string
	Title             *string
	Description       *string
	Naming            *string
	FQSchemaNaming    *bool
	EnumType          *string
	CircularDepth     *int
	DefaultResponse   *bool
	Validate          *bool
	BuildTag          *string // Kolla
	ResourceIDPattern *string // Kolla
}

const (
	infoURL            = "https://github.com/kollalabs/protoc-gen-openapi"
	BuildTagPublicDocs = "public_docs"
)

// In order to dynamically add google.rpc.Status responses we need
// to know the message descriptors for google.rpc.Status as well
// as google.protobuf.Any.
var statusProtoDesc = (&status_pb.Status{}).ProtoReflect().Descriptor()
var anyProtoDesc = (&any_pb.Any{}).ProtoReflect().Descriptor()

// OpenAPIv3Generator holds internal state needed to generate an OpenAPIv3 document for a transcoded Protocol Buffer service.
type OpenAPIv3Generator struct {
	conf   Configuration
	plugin *protogen.Plugin

	reflect           *OpenAPIv3Reflector
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

		reflect:           NewOpenAPIv3Reflector(conf),
		generatedSchemas:  make([]string, 0),
		linterRulePattern: regexp.MustCompile(`\(-- (?s:.)* --\)`), // Kolla
		pathPattern:       regexp.MustCompile("{([^=}]+)}"),
		namedPathPattern:  regexp.MustCompile("{([^{}=]+)=([^{}]+)}"),
	}
}

// Run runs the generator.
func (g *OpenAPIv3Generator) Run() error {
	d, err := g.buildDocumentV3()
	if err != nil {
		return err
	}
	bytes, err := d.YAMLValue("Generated with protoc-gen-openapi\n" + infoURL)
	if err != nil {
		return fmt.Errorf("failed to marshal yaml: %s", err.Error())
	}
	outputFile := g.plugin.NewGeneratedFile("openapi.yaml", "")
	outputFile.Write(bytes)
	return nil
}

// buildDocumentV3 builds an OpenAPIv3 document for a plugin request.
func (g *OpenAPIv3Generator) buildDocumentV3() (*v3.Document, error) {
	d := &v3.Document{}

	d.Openapi = "3.0.3"
	d.Info = &v3.Info{
		Version:     *g.conf.Version,
		Title:       *g.conf.Title,
		Description: *g.conf.Description,
	}

	g.reflect.assignSchemaNames(g.plugin.Files)

	d.Paths = &v3.Paths{}
	d.Components = &v3.Components{
		Schemas: &v3.SchemasOrReferences{
			AdditionalProperties: []*v3.NamedSchemaOrReference{},
		},
	}

	// Go through the files and add the services to the documents, keeping
	// track of which schemas are referenced in the response so we can
	// add them later.
	for _, file := range g.plugin.Files {
		if file.Generate {
			// Merge any `Document` annotations with the current
			extDocument := proto.GetExtension(file.Desc.Options(), v3.E_Document)
			if extDocument != nil {
				proto.Merge(d, extDocument.(*v3.Document))
			}

			if err := g.addPathsToDocumentV3(d, file); err != nil {
				return nil, err
			}
		}
	}

	// While we have required schemas left to generate, go through the files again
	// looking for the related message and adding them to the document if required.
	for len(g.reflect.requiredSchemas) > 0 {
		count := len(g.reflect.requiredSchemas)
		for _, file := range g.plugin.Files {
			g.addSchemasForMessagesToDocumentV3(d, file.Messages)
		}
		g.reflect.requiredSchemas = g.reflect.requiredSchemas[count:len(g.reflect.requiredSchemas)]
	}

	// If there is only 1 service, then use it's title for the
	// document, if the document is missing it.
	if len(d.Tags) == 1 {
		if d.Info.Title == "" && d.Tags[0].Name != "" {
			d.Info.Title = d.Tags[0].Name + " API"
		}
		if d.Info.Description == "" {
			d.Info.Description = d.Tags[0].Description
		}
		d.Tags[0].Description = ""
	}

	allServers := []string{}

	// If paths methods has servers, but they're all the same, then move servers to path level
	for _, path := range d.Paths.Path {
		servers := []string{}
		// Only 1 server will ever be set, per method, by the generator

		for _, op := range pathOperations(path.Value) {
			if len(op.Servers) == 1 {
				servers = appendUnique(servers, op.Servers[0].Url)
				allServers = appendUnique(allServers, op.Servers[0].Url)
			}
		}

		if len(servers) == 1 {
			path.Value.Servers = []*v3.Server{{Url: servers[0]}}
			for _, op := range pathOperations(path.Value) {
				op.Servers = nil
			}
		}
	}

	// Set all servers on API level
	if len(allServers) > 0 {
		d.Servers = []*v3.Server{}
		for _, server := range allServers {
			d.Servers = append(d.Servers, &v3.Server{Url: server})
		}
	}

	// If there is only 1 server, we can safely remove all path level servers
	if len(allServers) == 1 {
		for _, path := range d.Paths.Path {
			path.Value.Servers = nil
		}
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
	return d, nil
}

// pathOperations returns the operations set on a path item.
func pathOperations(item *v3.PathItem) []*v3.Operation {
	var ops []*v3.Operation
	for _, op := range []*v3.Operation{item.Get, item.Put, item.Post, item.Delete, item.Options, item.Head, item.Patch, item.Trace} {
		if op != nil {
			ops = append(ops, op)
		}
	}
	return ops
}

// filterMethodDescription returns the part after "|" in a "Summary | description" method comment.
func (g *OpenAPIv3Generator) filterMethodDescription(c protogen.Comments) string {
	split := strings.SplitN(string(c), "|", 2)
	if len(split) >= 2 {
		c = protogen.Comments(split[1])
	}
	return g.filterCommentString(c, false)
}

// filterCommentString removes line breaks and linter rules from comments.
func (g *OpenAPIv3Generator) filterCommentString(c protogen.Comments, removeNewLines bool) string {
	comment := string(c)
	if removeNewLines {
		comment = strings.Replace(comment, "\n", "", -1)
	}
	comment = g.linterRulePattern.ReplaceAllString(comment, "")
	return strings.TrimSpace(comment)
}

func (g *OpenAPIv3Generator) findField(name string, inMessage *protogen.Message) *protogen.Field {
	for _, field := range inMessage.Fields {
		if string(field.Desc.Name()) == name || string(field.Desc.JSONName()) == name {
			return field
		}
	}

	return nil
}

func (g *OpenAPIv3Generator) findAndFormatFieldName(name string, inMessage *protogen.Message) string {
	field := g.findField(name, inMessage)
	if field != nil {
		return g.reflect.formatFieldName(field.Desc)
	}

	return name
}

// Note that fields which are mapped to URL query parameters must have a primitive type
// or a repeated primitive type or a non-repeated message type.
// In the case of a repeated type, the parameter can be repeated in the URL as ...?param=A&param=B.
// In the case of a message type, each field of the message is mapped to a separate parameter,
// such as ...?foo.a=A&foo.b=B&foo.c=C.
//
// maps, Struct and Empty can NOT be used
// messages can have any number of sub messages - including circular (e.g. sub.subsub.sub.subsub.id)

// buildQueryParamsV3 extracts any valid query params, including sub and recursive messages
func (g *OpenAPIv3Generator) buildQueryParamsV3(field *protogen.Field) []*v3.ParameterOrReference {
	depths := map[string]int{}
	return g._buildQueryParamsV3(field, depths)
}

// depths counts how many times each field appears on the current recursion path
func (g *OpenAPIv3Generator) _buildQueryParamsV3(field *protogen.Field, depths map[string]int) []*v3.ParameterOrReference {
	parameters := []*v3.ParameterOrReference{}

	queryFieldName := g.reflect.formatFieldName(field.Desc)
	fieldDescription := g.filterCommentString(field.Comments.Leading, true)

	if field.Desc.IsMap() {
		// Map types are not allowed in query parameteres
		return parameters
	}

	if field.Desc.Kind() == protoreflect.MessageKind {
		typeName := g.reflect.fullMessageTypeName(field.Desc.Message())

		if typeName == ".google.protobuf.Value" {
			fieldSchema := g.reflect.schemaOrReferenceForField(field.Desc)

			parameters = append(parameters,
				&v3.ParameterOrReference{
					Oneof: &v3.ParameterOrReference_Parameter{
						Parameter: &v3.Parameter{
							Name:        queryFieldName,
							In:          "query",
							Description: fieldDescription,
							Required:    false,
							Schema:      fieldSchema,
						},
					},
				})
			return parameters
		} else if field.Desc.IsList() {
			// Only non-repeated message types are valid
			return parameters
		}

		// Represent field masks directly as strings (don't expand them).
		if typeName == ".google.protobuf.Duration" {
			fieldSchema := g.reflect.schemaOrReferenceForField(field.Desc)
			parameters = append(parameters,
				&v3.ParameterOrReference{
					Oneof: &v3.ParameterOrReference_Parameter{
						Parameter: &v3.Parameter{
							Name:        queryFieldName,
							In:          "query",
							Description: fieldDescription,
							Required:    false,
							Schema:      fieldSchema,
						},
					},
				})
			return parameters
		}

		// Represent field masks directly as strings (don't expand them).
		if typeName == ".google.protobuf.Timestamp" {
			fieldSchema := g.reflect.schemaOrReferenceForField(field.Desc)
			parameters = append(parameters,
				&v3.ParameterOrReference{
					Oneof: &v3.ParameterOrReference_Parameter{
						Parameter: &v3.Parameter{
							Name:        queryFieldName,
							In:          "query",
							Description: fieldDescription,
							Required:    false,
							Schema:      fieldSchema,
						},
					},
				})
			return parameters
		}

		// Represent field masks directly as strings (don't expand them).
		if typeName == ".google.protobuf.FieldMask" {
			fieldSchema := g.reflect.schemaOrReferenceForField(field.Desc)
			parameters = append(parameters,
				&v3.ParameterOrReference{
					Oneof: &v3.ParameterOrReference_Parameter{
						Parameter: &v3.Parameter{
							Name:        queryFieldName,
							In:          "query",
							Description: fieldDescription,
							Required:    false,
							Schema:      fieldSchema,
						},
					},
				})
			return parameters
		}

		// Sub messages are allowed, even circular, as long as the final type is a primitive.
		// Go through each of the sub message fields
		for _, subField := range field.Message.Fields {
			subFieldFullName := string(subField.Desc.FullName())
			if depths[subFieldFullName] < *g.conf.CircularDepth {
				// Count only the current path, so siblings sharing a type don't use up the depth.
				depths[subFieldFullName]++
				subParams := g._buildQueryParamsV3(subField, depths)
				depths[subFieldFullName]--
				for _, subParam := range subParams {
					if param, ok := subParam.Oneof.(*v3.ParameterOrReference_Parameter); ok {
						param.Parameter.Name = queryFieldName + "." + param.Parameter.Name
						parameters = append(parameters, subParam)
					}
				}
			}
		}

	} else if field.Desc.Kind() != protoreflect.GroupKind {
		// schemaOrReferenceForField also handles array types
		fieldSchema := g.reflect.schemaOrReferenceForField(field.Desc)
		if *g.conf.Validate { // Kolla
			g.addValidationRules(fieldSchema, field.Desc)
		}

		parameters = append(parameters,
			&v3.ParameterOrReference{
				Oneof: &v3.ParameterOrReference_Parameter{
					Parameter: &v3.Parameter{
						Name:        queryFieldName,
						In:          "query",
						Description: fieldDescription,
						Required:    false,
						Schema:      fieldSchema,
					},
				},
			})
	}

	return parameters
}

// buildOperationV3 constructs an operation for a set of values.
func (g *OpenAPIv3Generator) buildOperationV3(
	d *v3.Document,
	summary string, // Kolla
	operationID string,
	tagName string,
	description string,
	defaultHost string,
	path string,
	bodyField string,
	inputMessage *protogen.Message,
	outputMessage *protogen.Message,
	scopeParams []*open_api_extensions.Parameters, // Kolla
) (*v3.Operation, string, error) {
	// coveredParameters tracks the parameters that have been used in the body or path.
	coveredParameters := make([]string, 0)
	if bodyField != "" {
		coveredParameters = append(coveredParameters, bodyField)
	}
	// Initialize the list of operation parameters.
	parameters := []*v3.ParameterOrReference{}

	// Find simple path parameters like {id}
	if allMatches := g.pathPattern.FindAllStringSubmatch(path, -1); allMatches != nil {
		for _, matches := range allMatches {
			// Add the value to the list of covered parameters.
			coveredParameters = append(coveredParameters, matches[1])
			pathParameter := g.findAndFormatFieldName(matches[1], inputMessage)
			// Replace the whole placeholder so literal segments are untouched.
			path = strings.Replace(path, matches[0], "{"+pathParameter+"}", 1)

			// Add the path parameters to the operation parameters.
			var fieldSchema *v3.SchemaOrReference

			var fieldDescription string
			field := g.findField(pathParameter, inputMessage)
			if field != nil {
				fieldSchema = g.reflect.schemaOrReferenceForField(field.Desc)
				fieldDescription = g.filterCommentString(field.Comments.Leading, true)
			} else {
				// If field does not exist, it is safe to set it to string, as it is ignored downstream
				fieldSchema = &v3.SchemaOrReference{
					Oneof: &v3.SchemaOrReference_Schema{
						Schema: &v3.Schema{
							Type: "string",
						},
					},
				}
			}

			parameters = append(parameters,
				&v3.ParameterOrReference{
					Oneof: &v3.ParameterOrReference_Parameter{
						Parameter: &v3.Parameter{
							Name:        pathParameter,
							In:          "path",
							Description: fieldDescription,
							Required:    true,
							Schema:      fieldSchema,
						},
					},
				})
		}
	}

	// Find named path parameters like {name=shelves/*}
	for _, matches := range g.namedPathPattern.FindAllStringSubmatch(path, -1) {
		// Build a list of named path parameters.
		namedPathParameters := make([]string, 0)

		// Add the "name=" "name" value to the list of covered parameters.
		coveredParameters = append(coveredParameters, matches[1])
		// Convert the path from the starred form to use named path parameters.
		starredPath := matches[2]

		// A bare wildcard ({path=*} or {path=**}) is a single parameter; "**" may contain slashes.
		if starredPath == "*" || starredPath == "**" {
			pathParameter := g.findAndFormatFieldName(matches[1], inputMessage)
			path = strings.Replace(path, matches[0], "{"+pathParameter+"}", 1)

			fieldSchema := wk.NewStringSchema()
			var fieldDescription string
			if field := g.findField(matches[1], inputMessage); field != nil {
				fieldSchema = g.reflect.schemaOrReferenceForField(field.Desc)
				fieldDescription = g.filterCommentString(field.Comments.Leading, true)
			}
			multiSegment := starredPath == "**"
			if schema, ok := fieldSchema.Oneof.(*v3.SchemaOrReference_Schema); ok && multiSegment {
				schema.Schema.Pattern = ".+"
			}

			parameters = append(parameters,
				&v3.ParameterOrReference{
					Oneof: &v3.ParameterOrReference_Parameter{
						Parameter: &v3.Parameter{
							Name:          pathParameter,
							In:            "path",
							Description:   fieldDescription,
							Required:      true,
							AllowReserved: multiSegment,
							Schema:        fieldSchema,
						},
					},
				})
			continue
		}

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
	}

	// Kolla: custom headers.
	parameters = append(parameters, g.customHeaderParameters(scopeParams)...)

	// Add any unhandled fields in the request message as query parameters.
	if bodyField != "*" && string(inputMessage.Desc.FullName()) != "google.api.HttpBody" {
		for _, field := range inputMessage.Fields {
			fieldName := string(field.Desc.Name())
			if !contains(coveredParameters, fieldName) && fieldName != bodyField {
				fieldParams := g.buildQueryParamsV3(field)
				parameters = append(parameters, fieldParams...)
			}
		}
	}

	// Create the response.
	name, content := g.reflect.responseContentForMessage(outputMessage.Desc)
	responses := &v3.Responses{
		ResponseOrReference: []*v3.NamedResponseOrReference{
			{
				Name: name,
				Value: &v3.ResponseOrReference{
					Oneof: &v3.ResponseOrReference_Response{
						Response: &v3.Response{
							Description: "OK",
							Content:     content,
						},
					},
				},
			},
		},
	}

	// Add the default reponse if needed
	if *g.conf.DefaultResponse {
		anySchemaName := g.reflect.formatMessageName(anyProtoDesc)
		anySchema := wk.NewGoogleProtobufAnySchema(anySchemaName)
		g.addSchemaToDocumentV3(d, anySchema)

		statusSchemaName := g.reflect.formatMessageName(statusProtoDesc)
		statusSchema := wk.NewGoogleRpcStatusSchema(statusSchemaName, anySchemaName)
		g.addSchemaToDocumentV3(d, statusSchema)

		defaultResponse := &v3.NamedResponseOrReference{
			Name: "default",
			Value: &v3.ResponseOrReference{
				Oneof: &v3.ResponseOrReference_Response{
					Response: &v3.Response{
						Description: "Default error response",
						Content: wk.NewApplicationJsonMediaType(&v3.SchemaOrReference{
							Oneof: &v3.SchemaOrReference_Reference{
								Reference: &v3.Reference{XRef: "#/components/schemas/" + statusSchemaName}}}),
					},
				},
			},
		}

		responses.ResponseOrReference = append(responses.ResponseOrReference, defaultResponse)
	}

	// Kolla: custom responses.
	if err := g.addCustomResponses(d, responses, scopeParams, operationID); err != nil {
		return nil, "", err
	}

	// Create the operation.
	op := &v3.Operation{
		Tags:        []string{tagName},
		Description: description,
		Summary:     summary, // Kolla
		OperationId: operationID,
		Parameters:  parameters,
		Responses:   responses,
	}

	if defaultHost != "" {
		hostURL, err := url.Parse(defaultHost)
		if err == nil {
			hostURL.Scheme = "https"
			op.Servers = append(op.Servers, &v3.Server{Url: hostURL.String()})
		}
	}

	// If a body field is specified, we need to pass a message as the request body.
	if bodyField != "" {
		var requestSchema *v3.SchemaOrReference

		if bodyField == "*" {
			// Pass the entire request message as the request body.
			requestSchema = g.reflect.schemaOrReferenceForMessage(inputMessage.Desc)

		} else {
			// If body refers to a message field, use that type.
			for _, field := range inputMessage.Fields {
				if string(field.Desc.Name()) == bodyField {
					switch field.Desc.Kind() {
					case protoreflect.StringKind:
						requestSchema = &v3.SchemaOrReference{
							Oneof: &v3.SchemaOrReference_Schema{
								Schema: &v3.Schema{
									Type: "string",
								},
							},
						}

					case protoreflect.MessageKind:
						requestSchema = g.reflect.schemaOrReferenceForMessage(field.Message.Desc)

					default:
						log.Printf("unsupported field type %+v", field.Desc)
					}
					break
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
	return op, path, nil
}

// scopeParameters returns the file, service and method options that apply, least specific first.
// Method build_tags gate the method itself, so method options always apply.
func (g *OpenAPIv3Generator) scopeParameters(fileParams, serviceParams, methodParams *open_api_extensions.Parameters) []*open_api_extensions.Parameters {
	var scope []*open_api_extensions.Parameters
	for _, params := range []*open_api_extensions.Parameters{fileParams, serviceParams} {
		if params != nil && g.matchesBuildTag(params.BuildTags) {
			scope = append(scope, params)
		}
	}
	if methodParams != nil {
		scope = append(scope, methodParams)
	}
	return scope
}

// customHeaderParameters builds header parameters; more specific headers replace same-named ones.
func (g *OpenAPIv3Generator) customHeaderParameters(scopeParams []*open_api_extensions.Parameters) []*v3.ParameterOrReference {
	var headers []*open_api_extensions.Header
	for _, params := range scopeParams {
		for _, header := range params.Headers {
			headers = slices.DeleteFunc(headers, func(h *open_api_extensions.Header) bool {
				return h.GetName() == header.GetName()
			})
			headers = append(headers, header)
		}
	}

	parameters := []*v3.ParameterOrReference{}
	for _, header := range headers {
		name := header.GetName()
		headerDescription := header.GetDescription()
		if header.Description == nil {
			headerDescription = "Custom header: " + name
		}

		parameter := &v3.Parameter{
			Name:        name,
			In:          "header",
			Description: headerDescription,
			Required:    header.GetRequired(),
			Schema: &v3.SchemaOrReference{
				Oneof: &v3.SchemaOrReference_Schema{
					Schema: &v3.Schema{
						Type:    "string",
						Pattern: header.GetPattern(),
					},
				},
			},
		}

		if header.GetExample() != "" {
			bytesValue := &wrappers.BytesValue{Value: []byte(header.GetExample())}
			exampleValue := &anypb.Any{}
			err := anypb.MarshalFrom(exampleValue, bytesValue, proto.MarshalOptions{})
			if err != nil {
				fmt.Println("Error marshalling example value: ", err)
			} else {
				parameter.Example = &v3.Any{Value: exampleValue, Yaml: header.GetExample()}
			}
		}

		parameters = append(parameters, &v3.ParameterOrReference{
			Oneof: &v3.ParameterOrReference_Parameter{Parameter: parameter},
		})
	}
	return parameters
}

// addCustomResponses adds custom_responses, replacing any existing response with the same code.
func (g *OpenAPIv3Generator) addCustomResponses(d *v3.Document, responses *v3.Responses, scopeParams []*open_api_extensions.Parameters, operationID string) error {
	custom := map[string]*open_api_extensions.Response{}
	for _, params := range scopeParams {
		maps.Copy(custom, params.GetCustomResponses())
	}
	if len(custom) == 0 {
		return nil
	}

	for code, response := range custom {
		description := response.GetDescription()
		if description == "" {
			if status, err := strconv.Atoi(code); err == nil {
				description = http.StatusText(status)
			}
		}
		if description == "" {
			description = "Custom response"
		}

		var content *v3.MediaTypes
		if ref := response.GetMessageRef(); ref != "" {
			message, err := g.findCustomResponseMessage(d, ref)
			if err != nil {
				return fmt.Errorf("%s: custom response %q: %w", operationID, code, err)
			}
			_, content = g.reflect.responseContentForMessage(message)
		}

		responses.ResponseOrReference = slices.DeleteFunc(responses.ResponseOrReference, func(r *v3.NamedResponseOrReference) bool {
			return r.Name == code
		})
		responses.ResponseOrReference = append(responses.ResponseOrReference, &v3.NamedResponseOrReference{
			Name: code,
			Value: &v3.ResponseOrReference{
				Oneof: &v3.ResponseOrReference_Response{
					Response: &v3.Response{Description: description, Content: content},
				},
			},
		})
	}

	// Status codes in order, with the catch-all "default" last.
	slices.SortFunc(responses.ResponseOrReference, func(a, b *v3.NamedResponseOrReference) int {
		if (a.Name == "default") != (b.Name == "default") {
			if a.Name == "default" {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return nil
}

// findCustomResponseMessage resolves a message_ref to one of the plugin's messages.
func (g *OpenAPIv3Generator) findCustomResponseMessage(d *v3.Document, ref string) (protoreflect.MessageDescriptor, error) {
	name := protoreflect.FullName(strings.TrimPrefix(ref, "."))
	for _, file := range g.plugin.Files {
		if message := findMessage(file.Messages, name); message != nil {
			// google.rpc.Status uses a built-in schema.
			if name == statusProtoDesc.FullName() {
				anySchemaName := g.reflect.formatMessageName(anyProtoDesc)
				g.addSchemaToDocumentV3(d, wk.NewGoogleProtobufAnySchema(anySchemaName))
				g.addSchemaToDocumentV3(d, wk.NewGoogleRpcStatusSchema(g.reflect.formatMessageName(statusProtoDesc), anySchemaName))
			}
			return message.Desc, nil
		}
	}
	return nil, fmt.Errorf("message_ref %q does not match any message; check the name and that its file is imported", ref)
}

func findMessage(messages []*protogen.Message, name protoreflect.FullName) *protogen.Message {
	for _, message := range messages {
		if message.Desc.FullName() == name {
			return message
		}
		if nested := findMessage(message.Messages, name); nested != nil {
			return nested
		}
	}
	return nil
}

// matchesBuildTag reports whether options apply to the current build tag; untagged options always do.
func (g *OpenAPIv3Generator) matchesBuildTag(tags []string) bool {
	return len(tags) == 0 || slices.Contains(tags, *g.conf.BuildTag)
}

// addOperationToDocumentV3 adds an operation to the specified path/method.
func (g *OpenAPIv3Generator) addOperationToDocumentV3(d *v3.Document, op *v3.Operation, path string, methodName string) {
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
	case "HEAD":
		selectedPathItem.Value.Head = op
	case "OPTIONS":
		selectedPathItem.Value.Options = op
	case "TRACE":
		selectedPathItem.Value.Trace = op
	}
}

// httpBinding is one HTTP mapping of an RPC method.
type httpBinding struct {
	method string
	path   string
	body   string
}

// httpBindings returns rule's binding and additional_bindings, skipping ones OpenAPI can't represent.
func httpBindings(rule *annotations.HttpRule, methodFullName string) []httpBinding {
	var bindings []httpBinding
	for _, r := range append([]*annotations.HttpRule{rule}, rule.AdditionalBindings...) {
		b := httpBinding{body: r.Body}
		switch pattern := r.Pattern.(type) {
		case *annotations.HttpRule_Get:
			b.method, b.path = "GET", pattern.Get
		case *annotations.HttpRule_Post:
			b.method, b.path = "POST", pattern.Post
		case *annotations.HttpRule_Put:
			b.method, b.path = "PUT", pattern.Put
		case *annotations.HttpRule_Delete:
			b.method, b.path = "DELETE", pattern.Delete
		case *annotations.HttpRule_Patch:
			b.method, b.path = "PATCH", pattern.Patch
		case *annotations.HttpRule_Custom:
			switch kind := strings.ToUpper(pattern.Custom.GetKind()); kind {
			case "HEAD", "OPTIONS", "TRACE":
				b.method, b.path = kind, pattern.Custom.GetPath()
			default:
				log.Printf("skipping %s: custom HTTP method %q is not supported by OpenAPI", methodFullName, pattern.Custom.GetKind())
				continue
			}
		default:
			log.Printf("skipping %s: HTTP rule has no pattern", methodFullName)
			continue
		}
		bindings = append(bindings, b)
	}
	return bindings
}

// addPathsToDocumentV3 adds paths from a specified file descriptor.
func (g *OpenAPIv3Generator) addPathsToDocumentV3(d *v3.Document, file *protogen.File) error {
	var fileParams *open_api_extensions.Parameters
	fileParamsOpts := proto.GetExtension(file.Desc.Options(), open_api_extensions.E_FileParams)
	if fileParamsOpts != nil && fileParamsOpts != open_api_extensions.E_FileParams.InterfaceOf(open_api_extensions.E_FileParams.Zero()) {
		fileParams = fileParamsOpts.(*open_api_extensions.Parameters)
	}

	for _, service := range file.Services {
		annotationsCount := 0
		serviceHeadersOpts := proto.GetExtension(service.Desc.Options(), open_api_extensions.E_ServiceParams)
		var params *open_api_extensions.Parameters
		if serviceHeadersOpts != nil && serviceHeadersOpts != open_api_extensions.E_ServiceParams.InterfaceOf(open_api_extensions.E_ServiceParams.Zero()) {
			params = serviceHeadersOpts.(*open_api_extensions.Parameters)
		}
		for _, method := range service.Methods {
			comment := g.filterMethodDescription(method.Comments.Leading)
			inputMessage := method.Input
			outputMessage := method.Output
			summary := g.filterCommentStringForSummary(method.Comments.Leading, method.GoName) // Kolla
			operationID := service.GoName + "_" + method.GoName

			var bindings []httpBinding

			var methodParams *open_api_extensions.Parameters
			methodOptionsParams := proto.GetExtension(method.Desc.Options(), open_api_extensions.E_MethodParams)
			if methodOptionsParams != nil && methodOptionsParams != open_api_extensions.E_MethodParams.InterfaceOf(open_api_extensions.E_MethodParams.Zero()) {
				methodParams = methodOptionsParams.(*open_api_extensions.Parameters)
			}

			extHTTP := proto.GetExtension(method.Desc.Options(), annotations.E_Http)
			if extHTTP != nil && extHTTP != annotations.E_Http.InterfaceOf(annotations.E_Http.Zero()) {
				annotationsCount++

				rule := extHTTP.(*annotations.HttpRule)
				bindings = httpBindings(rule, string(method.Desc.FullName()))
			}
			// If build tags exist, and a built tag is set in the protoc command, then only generate the method if the build tag is set
			doGenerate := true
			// If a build tag is set in the protoc command, then only generate the method if the build tag is set on the proto options
			if *g.conf.BuildTag != "" && *g.conf.BuildTag == BuildTagPublicDocs {
				doGenerate = false
			}

			if methodParams != nil && methodParams.BuildTags != nil && len(methodParams.BuildTags) > 0 {
				for _, tag := range methodParams.BuildTags {
					if tag == *g.conf.BuildTag {
						doGenerate = true
						break
					}
				}
			}

			if doGenerate {
				defaultHost := proto.GetExtension(service.Desc.Options(), annotations.E_DefaultHost).(string)

				scopeParams := g.scopeParameters(fileParams, params, methodParams)

				for i, binding := range bindings {
					op, path2, err := g.buildOperationV3(
						d, summary, operationID, service.GoName, comment, defaultHost, binding.path, binding.body, inputMessage, outputMessage, scopeParams)
					if err != nil {
						return err
					}

					// Merge any `Operation` annotations with the current
					extOperation := proto.GetExtension(method.Desc.Options(), v3.E_Operation)
					if extOperation != nil {
						proto.Merge(op, extOperation.(*v3.Operation))
					}
					// Operation IDs must be unique.
					if i > 0 {
						op.OperationId = fmt.Sprintf("%s_%d", op.OperationId, i+1)
					}

					g.addOperationToDocumentV3(d, op, path2, binding.method)
				}
			}
		}

		if annotationsCount > 0 {
			comment := g.filterCommentString(service.Comments.Leading, false)
			d.Tags = append(d.Tags, &v3.Tag{Name: service.GoName, Description: comment})
		}
	}
	return nil
}

// addSchemaForMessageToDocumentV3 adds the schema to the document if required
func (g *OpenAPIv3Generator) addSchemaToDocumentV3(d *v3.Document, schema *v3.NamedSchemaOrReference) {
	if contains(g.generatedSchemas, schema.Name) {
		return
	}
	g.generatedSchemas = append(g.generatedSchemas, schema.Name)
	d.Components.Schemas.AdditionalProperties = append(d.Components.Schemas.AdditionalProperties, schema)
}

// addSchemasForMessagesToDocumentV3 adds info from one file descriptor.
func (g *OpenAPIv3Generator) addSchemasForMessagesToDocumentV3(d *v3.Document, messages []*protogen.Message) {
	// For each message, generate a definition.
	for _, message := range messages {
		if message.Messages != nil {
			g.addSchemasForMessagesToDocumentV3(d, message.Messages)
		}

		schemaName := g.reflect.formatMessageName(message.Desc)

		// Only generate this if we need it and haven't already generated it.
		if !contains(g.reflect.requiredSchemas, schemaName) ||
			contains(g.generatedSchemas, schemaName) {
			continue
		}

		// Kolla
		xt := annotations.E_Resource
		extension := proto.GetExtension(message.Desc.Options(), xt)
		pattern := ""
		if extension != nil && extension != xt.InterfaceOf(xt.Zero()) {
			rule := extension.(*annotations.ResourceDescriptor)
			if len(rule.Pattern) > 0 {
				pattern = rule.Pattern[0]
			}
		}
		// Kolla

		typeName := g.reflect.fullMessageTypeName(message.Desc)
		messageDescription := g.filterCommentString(message.Comments.Leading, true)

		// `google.protobuf.Value` and `google.protobuf.Any` have special JSON transcoding
		// so we can't just reflect on the message descriptor.
		if typeName == ".google.protobuf.Value" {
			g.addSchemaToDocumentV3(d, wk.NewGoogleProtobufValueSchema(schemaName))
			continue
		} else if typeName == ".google.protobuf.Any" {
			g.addSchemaToDocumentV3(d, wk.NewGoogleProtobufAnySchema(schemaName))
			continue
		} else if typeName == ".google.rpc.Status" {
			anySchemaName := g.reflect.formatMessageName(anyProtoDesc)
			g.addSchemaToDocumentV3(d, wk.NewGoogleProtobufAnySchema(anySchemaName))
			g.addSchemaToDocumentV3(d, wk.NewGoogleRpcStatusSchema(schemaName, anySchemaName))
			continue
		}

		// Build an array holding the fields of the message.
		definitionProperties := &v3.Properties{
			AdditionalProperties: make([]*v3.NamedSchemaOrReference, 0),
		}

		var required []string
		for _, field := range message.Fields {
			// Check the field annotations to see if this is a readonly or writeonly field.
			inputOnly := false
			outputOnly := false
			extension := proto.GetExtension(field.Desc.Options(), annotations.E_FieldBehavior)
			if extension != nil {
				switch v := extension.(type) {
				case []annotations.FieldBehavior:
					for _, vv := range v {
						switch vv {
						case annotations.FieldBehavior_OUTPUT_ONLY:
							outputOnly = true
						case annotations.FieldBehavior_INPUT_ONLY:
							inputOnly = true
						case annotations.FieldBehavior_REQUIRED:
							required = append(required, g.reflect.formatFieldName(field.Desc))
						}
					}
				default:
					log.Printf("unsupported extension type %T", extension)
				}
			}

			// The field is either described by a reference or a schema.
			fieldSchema := g.reflect.schemaOrReferenceForField(field.Desc)
			if fieldSchema == nil {
				continue
			}

			if schema, ok := fieldSchema.Oneof.(*v3.SchemaOrReference_Schema); ok {
				if field.Desc.Name() == "name" && pattern != "" && *g.conf.ResourceIDPattern != "" { // Kolla
					schema.Schema.Pattern = resourceNamePattern(pattern, *g.conf.ResourceIDPattern)
				}
				// Get the field description from the comments.
				schema.Schema.Description = g.filterCommentString(field.Comments.Leading, true)
				if outputOnly {
					schema.Schema.ReadOnly = true
				}
				if inputOnly {
					schema.Schema.WriteOnly = true
				}
			}

			// Kolla
			if *g.conf.Validate {
				g.addValidationRules(fieldSchema, field.Desc)
			}

			definitionProperties.AdditionalProperties = append(
				definitionProperties.AdditionalProperties,
				&v3.NamedSchemaOrReference{
					Name:  g.reflect.formatFieldName(field.Desc),
					Value: fieldSchema,
				},
			)
		}

		// Add the schema to the components.schema list.
		g.addSchemaToDocumentV3(d, &v3.NamedSchemaOrReference{
			Name: schemaName,
			Value: &v3.SchemaOrReference{
				Oneof: &v3.SchemaOrReference_Schema{
					Schema: &v3.Schema{
						Type:        "object",
						Description: messageDescription,
						Properties:  definitionProperties,
						Required:    required,
					},
				},
			},
		})
	}
}
