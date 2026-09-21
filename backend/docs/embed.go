package apidocs

import _ "embed"

//go:embed openapi.json
var OpenAPIJSON []byte

//go:embed swagger.html
var SwaggerHTML []byte
