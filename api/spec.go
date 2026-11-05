package api

import _ "embed"

// OpenAPISpec is served at /openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
