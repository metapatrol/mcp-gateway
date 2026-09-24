// Package openapi holds the OpenAPI descriptions of mcp-gateway's HTTP APIs,
// embedded, so a process serves the very file its code was generated from.
package openapi

import (
	_ "embed"

	"sigs.k8s.io/yaml"
)

// InspectYAML is api/openapi/inspect.yaml as written.
//
//go:embed inspect.yaml
var InspectYAML []byte

// InspectJSON is the same document as JSON.
func InspectJSON() ([]byte, error) { return yaml.YAMLToJSON(InspectYAML) }
