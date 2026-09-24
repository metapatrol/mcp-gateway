package inspect

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"forgejo.metapatrol.local/platform/mcp-gateway/api/openapi"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
)

// The API's own description is public: what the API offers is no secret, what
// it answers is. It is the embedded file the server interface was generated
// from, so it cannot describe another API than the one serving it.

func (s *Server) GetOpenAPIYAML(context.Context, api.GetOpenAPIYAMLRequestObject) (api.GetOpenAPIYAMLResponseObject, error) {
	return api.GetOpenAPIYAML200ApplicationyamlResponse{
		Body:          bytes.NewReader(openapi.InspectYAML),
		ContentLength: int64(len(openapi.InspectYAML)),
	}, nil
}

func (s *Server) GetOpenAPIJSON(context.Context, api.GetOpenAPIJSONRequestObject) (api.GetOpenAPIJSONResponseObject, error) {
	doc, err := openapi.InspectJSON()
	if err != nil {
		return nil, err
	}
	return descriptionJSON(doc), nil
}

// descriptionJSON writes the document as it is. The generated response type
// is declared over json.RawMessage and so loses its MarshalJSON: it would be
// encoded as a base64 string.
type descriptionJSON []byte

func (d descriptionJSON) VisitGetOpenAPIJSONResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(d)
	return err
}

// publicPaths are answered without a token.
var publicPaths = []string{BasePath + "/openapi.yaml", BasePath + "/openapi.json"}

// APICatalogPath is where RFC 9727 has an API catalog: a linkset naming the
// APIs a host serves and where their descriptions are.
const APICatalogPath = "/.well-known/api-catalog"

// apiCatalog answers the RFC 9727 API catalog. It lies outside the inspect
// API — it is the well-known entry point to it — and so outside its spec.
func apiCatalog(w http.ResponseWriter, r *http.Request) {
	catalog := map[string]any{
		"linkset": []map[string]any{{
			"anchor": BasePath,
			"service-desc": []map[string]string{
				{"href": BasePath + "/openapi.json", "type": "application/vnd.oai.openapi+json;version=3.0"},
				{"href": BasePath + "/openapi.yaml", "type": "application/vnd.oai.openapi;version=3.0"},
			},
		}},
	}
	w.Header().Set("Content-Type", `application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"`)
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(catalog)
}
