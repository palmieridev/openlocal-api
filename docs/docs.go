package docs

import _ "embed"

// OpenAPIYAML is the Openlocal OpenAPI document served by the API.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte
