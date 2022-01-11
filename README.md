# protoc-gen-openapi

Contains a protoc plugin that generates openapi v3 documents

Installation:
        go install github.com/kollalabs/protoc-gen-openapi
    
Usage:

	protoc sample.proto -I. --openapi_out=.

This runs the plugin for a file named `sample.proto` which 
refers to additional .proto files in the same directory as
`sample.proto`. Output is written to the current directory.

