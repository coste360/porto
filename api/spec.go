// Package apispec embeds the OpenAPI contract so that the server can derive per-operation policies (accepted
// authentication methods, CSRF) from the same document the code is generated from.
package apispec

import _ "embed"

// OpenAPI is the content of api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
