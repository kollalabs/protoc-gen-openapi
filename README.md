# protoc-gen-openapi

Contains a protoc plugin that generates openapi v3 documents

**Forked from [github.com/google/gnostic/cmd/protoc-gen-openapi](https://github.com/google/gnostic/tree/main/cmd/protoc-gen-openapi)**

Installation:

    go install github.com/kollalabs/protoc-gen-openapi@latest

Usage:

    protoc sample.proto -I. --openapi_out=version=1.2.3:.

## Testing

To see output during tests use `log.Print*`

```
go clean -testcache && go test
```

## Added Features
We have added some features that the Gnostic team most likely doesn't want to add :-)
Some are fairly Kolla specific, sorry. We try to hide Kolla specific functionality
in a way that won't trip anyone up.

* [Better Enum Support](#better-enum-support)
* [Summary Field](#summary-field)
* [Validation (protoc-gen-validate)](#validation)
* [Google Field Behavior Annotations](#google-field-behavior-annotations)
* [OAS3 header support](#oas3-header-support)
* [Custom responses](#custom-responses)
* [Resource name pattern](#resource-name-pattern)

### Better Enum Support
Enums work better by using string values of proto enums instead of ints.

### Summary Field

Sometimes you want more control over certain properties in the OpenAPI manifest. In our
case we wanted to use the `summary` property on routes to look nice for generating
documentation from the OpenAPI manifest. Normally the summary comes simply from the
name of the route. We added a feature that parses the comment over the proto service
method and looks for a pipe character ("`|`") and if it sees it, it will take anything to
the left of it and put it in the `summary` field, and anything to the right of it will
be the `description`. If no pipe is found it puts the whole comment in the description
like normal. From `/examples/tests/summary/message.proto`:

```proto
service Messaging {
    // Update Message Summary | This function updates a message.
    rpc UpdateMessage(Message) returns(Message) {
        option(google.api.http) = {
            patch: "/v1/messages/{message_id}"
            body: "text"
        };
    }
}
```

It generates the following OpenAPI:

```yaml
#...
paths:
    /v1/messages/{message_id}:
        patch:
            tags:
                - Messaging
            summary: Update Message Summary # Look at this beautiful summary...
            description: This function updates a message.
#...
```

### Validation

We added partial support for `protoc-gen-validate` annotations

OpenAPI spec allows for a small handful of input validation configurations.
Proto has an awesome plugin called `protoc-gen-validate` for generating validation code in
Go, Java, C++, etc. We took those same annotations and added support in this project
for them.

Usage: add `validate=true` to protoc command.

`protoc sample.proto -I. --openapi_out=version=1.2.3,validate=true:.`

#### Example

```proto
message Message {
    string message_id = 1;
    string text = 2 [(validate.rules)= {
        string: {
            uri:true,
            max_len:45,
            min_len:1
        }
    }];
    int64 mynum = 3 [(validate.rules).int64 = {gte:1, lte:30}];
}

```

outputs:

```yaml
components:
    schemas:
        Message:
            properties:
                message_id:
                    type: string
                text:
                    maxLength: 45
                    minLength: 1
                    type: string
                    format: uri
                mynum:
                    maximum: !!float 30
                    minimum: !!float 1
                    type: integer
                    format: int64

```

#### Supported Validators

String
- uri
- uuid
- email
- ipv4
- ipv6
- max_len
- min_len

Numeric (all int, uint, sint, fixed, float and double types)
- const
- gt, gte (`minimum`, `exclusiveMinimum`)
- lt, lte (`maximum`, `exclusiveMaximum`)

Enum
- const
- in
- not_in

Repeated
- min_items
- max_items
- items (scalar item rules)

Adding more can easily be done in the function `addValidationRules` in `/generator/validate.go`

### Google Field Behavior Annotations

* `(google.api.field_behavior) = REQUIRED` will add the field to the required list in the openAPI schema
* `(google.api.field_behavior) = OUTPUT_ONLY` will add the `readOnly` property to the field
* `(google.api.field_behavior) = INPUT_ONLY` will add the `writeOnly` property to the field
* TODO: `(google.api.field_behavior) = IMMUTABLE` will add the `x-createOnly` property to the field (not supported by openapi yet)

### OAS3 header support

Add header parameters with `openapi.file_params`, `openapi.service_params` or `openapi.method_params`
(see [openapi/annotations.proto](openapi/annotations.proto)). A method header replaces a service or file header
with the same name.

File and service options only apply when their `build_tags` (if any) include the `build_tag` plugin option.
A method's `build_tags` decide whether the method is generated at all.

### Custom responses

Add responses to every operation in a file, service or method with `custom_responses`, keyed by status code.
A response replaces a generated one with the same code, such as the `google.rpc.Status` `default` response.
Build tags apply as for headers.

```proto
option (openapi.service_params) = {
    custom_responses: {
        key: "403"
        value: {
            description: "Forbidden"
            message_ref: "my.pkg.v1.ErrorResponse"
        }
    }
};
```

### Resource name pattern

`name` fields of messages with a `google.api.resource` pattern get a regex `pattern`, matching each variable
with `resource_id_pattern` (default `[a-z2-7]{26}`). Set `resource_id_pattern=` to omit it.
